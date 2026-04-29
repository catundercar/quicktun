# 进程管理（控制面 supervisor）

> 控制面用 Go `os/exec` 直接管理子进程：rathole-server（每个 project 一个）+ quicktun-auth-proxy（一个）。

## 1. 为什么不用 systemd template / containerd / docker

| 选项 | 评价 |
|---|---|
| systemd template | 多了一层文件 + reload 流程；进程状态查不直观 |
| containerd / docker | 多一个运行时依赖；rathole 单二进制不需要 fs 隔离 |
| **Go `os/exec` 子进程** | 零新依赖；进程状态在控制面内存可观测；崩溃事件可以直接接进 audit_log |

控制面**自身**由 systemd 拉起（保证开机自启 + 崩溃重启），但子进程在控制面进程内 supervise。

## 2. Supervisor 接口

```go
package supervisor

import (
    "context"
    "os/exec"
    "sync"
    "syscall"
    "time"

    "go.uber.org/zap"
)

type Spec struct {
    Name        string             // logical name, e.g. "rathole-clinic-network"
    Binary      string             // /usr/local/bin/rathole
    Args        []string           // [config_path]
    Env         []string
    User        string             // 切换到的 OS 用户
    WorkDir     string
    OnLog       func(line, src string) // stdout/stderr 行回调（送 zap）
    OnExit      func(err error)
}

type Supervisor struct {
    spec   Spec
    cmd    *exec.Cmd
    logger *zap.Logger
    mu     sync.Mutex
    stopCh chan struct{}
}

func New(spec Spec, logger *zap.Logger) *Supervisor { ... }

// Run 启动循环：崩了指数退避重启，max 30s。Block until ctx cancelled.
func (s *Supervisor) Run(ctx context.Context) {
    backoff := time.Second
    for {
        select { case <-ctx.Done(): return; default: }

        s.mu.Lock()
        s.cmd = exec.CommandContext(ctx, s.spec.Binary, s.spec.Args...)
        s.cmd.Env = s.spec.Env
        s.cmd.Dir = s.spec.WorkDir
        s.cmd.SysProcAttr = platformSysProcAttr(s.spec.User)
        stdout, _ := s.cmd.StdoutPipe()
        stderr, _ := s.cmd.StderrPipe()
        if err := s.cmd.Start(); err != nil {
            s.mu.Unlock()
            s.logger.Error("supervisor: start failed", zap.String("name", s.spec.Name), zap.Error(err))
            sleep(ctx, backoff)
            backoff = nextBackoff(backoff)
            continue
        }
        s.mu.Unlock()

        go pipeToCallback(stdout, "stdout", s.spec.OnLog)
        go pipeToCallback(stderr, "stderr", s.spec.OnLog)

        err := s.cmd.Wait()
        s.spec.OnExit(err)
        if ctx.Err() != nil { return }

        s.logger.Warn("supervisor: child exited",
            zap.String("name", s.spec.Name),
            zap.Error(err),
            zap.Duration("backoff", backoff))
        sleep(ctx, backoff)
        backoff = nextBackoff(backoff)
    }
}

// Stop graceful: SIGTERM, then SIGKILL after 5s.
func (s *Supervisor) Stop() error {
    s.mu.Lock()
    defer s.mu.Unlock()
    if s.cmd == nil || s.cmd.Process == nil { return nil }
    _ = s.cmd.Process.Signal(syscall.SIGTERM)
    done := make(chan struct{})
    go func() { _ = s.cmd.Wait(); close(done) }()
    select {
    case <-done:
    case <-time.After(5 * time.Second):
        _ = s.cmd.Process.Kill()
    }
    return nil
}
```

## 3. 平台抽象（关键）

`platformSysProcAttr` 跨平台关键点：

### 3.1 Linux

```go
//go:build linux
package supervisor

import "syscall"

func platformSysProcAttr(user string) *syscall.SysProcAttr {
    spa := &syscall.SysProcAttr{
        Pdeathsig: syscall.SIGTERM, // ★ 控制面崩了子进程跟着死
        Setpgid:   true,            // ★ 独立进程组，方便整组 kill
    }
    if user != "" {
        uid, gid := lookupUser(user)
        spa.Credential = &syscall.Credential{Uid: uid, Gid: gid}
    }
    return spa
}
```

### 3.2 Windows

Windows 没有 Pdeathsig。要用 **Job Object + JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE**。

```go
//go:build windows
package supervisor

// 推荐用 github.com/kolesnikovae/go-winjob
//   - 创建 JobObject
//   - 设置 KILL_ON_JOB_CLOSE
//   - 子进程启动后 AssignProcessToJobObject
// 控制面进程退出 → Job Object 句柄关闭 → 子进程被 kernel 杀掉
```

### 3.3 macOS（仅 dev）

仅本地 dev 用，没 Pdeathsig。控制面崩溃 → 孤儿子进程需要手动 kill。dev 工作区可以接受。

## 4. 启动 / 重启策略

### 4.1 控制面启动时重建状态

```go
// service.Start
projects := db.Find(&[]Project{{Status: "active"}})
for _, p := range projects {
    if p.Backend == "rathole" {
        rendered := renderRatholeServerConfig(p)
        os.WriteFile(configPath(p), rendered, 0600)
        sup := supervisor.New(supervisor.Spec{
            Name:    "rathole-" + p.Slug,
            Binary:  "/usr/local/bin/rathole",
            Args:    []string{configPath(p)},
            User:    "quicktun",
            OnLog:   func(line, src string) { zapWithProject(p.ID, src).Info(line) },
            OnExit:  func(err error) { audit.LogProcessExit(p.ID, err) },
        }, logger)
        go sup.Run(ctx)
        registry.Add(p.ID, sup)
    }
}
// auth-proxy 一个全局
authSup := supervisor.New(...)
go authSup.Run(ctx)
```

### 4.2 配置变更触发重启

rathole 不支持 SIGHUP reload，必须重启进程。

```go
func (m *Manager) ApplyProjectConfig(p Project) error {
    rendered := renderRatholeServerConfig(p)
    if !changed(p, rendered) { return nil }
    
    os.WriteFile(configPath(p), rendered, 0600)
    sup := registry.Get(p.ID)
    sup.Restart()  // 内部：Stop() + 下一轮 Run loop 自动起新 binary（用新 config 文件）
}
```

### 4.3 退避算法

```go
func nextBackoff(b time.Duration) time.Duration {
    next := b * 2
    if next > 30 * time.Second { return 30 * time.Second }
    return next
}
```

加 jitter 避免雪崩（多 project 同时崩同时重启）：

```go
sleep(ctx, b + time.Duration(rand.Int63n(int64(b/2))))
```

## 5. Graceful shutdown

```go
// 控制面收到 SIGTERM
ctx.Cancel()  // ← 所有 supervisor.Run 退出
for _, sup := range registry.All() {
    sup.Stop()  // 5s timeout
}
controlPlane.Close()
db.Close()
```

systemd 单元配 `TimeoutStopSec=10`，留够时间但不会无限等。

## 6. 日志（zap + lumberjack）

子进程 stdout/stderr 通过 `OnLog` 回调进入 zap：

```go
fields := []zap.Field{
    zap.String("project", p.Slug),
    zap.Uint64("project_id", p.ID),
    zap.String("source", "rathole-stdout"),  // 或 "rathole-stderr"
}
projectLogger := baseLogger.With(fields...)

OnLog: func(line, src string) {
    projectLogger.Info(line, zap.String("source", "rathole-"+src))
}
```

zap 配置：

```go
core := zapcore.NewCore(
    zapcore.NewJSONEncoder(zap.NewProductionEncoderConfig()),
    zapcore.AddSync(&lumberjack.Logger{
        Filename:   "/var/log/quicktun/control-plane.log",
        MaxSize:    100, // MB
        MaxBackups: 7,
        MaxAge:     30,  // days
        Compress:   true,
    }),
    zap.InfoLevel,
)
logger := zap.New(core)
```

所有项目 relay 日志 + 控制面自身日志在**同一个文件**，用 `project_id` / `source` 字段过滤。

## 7. 监控 hook

`OnExit` 回调上报到 `audit_logs`，便于事后查"哪个 project 的 rathole 在什么时间崩过几次"：

```go
OnExit: func(err error) {
    audit.Append(AuditLog{
        ProjectID: &p.ID,
        Action:    "process_exit",
        Target:    "rathole-server",
        ExtraJSON: marshalJSON(map[string]any{
            "exit_code": exitCode(err),
            "error":     errString(err),
        }),
    })
}
```

CLI 提供：

```bash
quicktun status --project=clinic-network
# rathole-server: running (pid 12345, uptime 3h)
# crashes last 24h: 0
```

## 8. 测试

- 单元测试 supervisor loop：mock binary（写一个 sleep+exit 的小程序）
- 集成测试：起真实 rathole，kill 之后断言 supervisor 重启
- 跨平台：CI 矩阵 ubuntu-latest + windows-latest，跑同一套测试
