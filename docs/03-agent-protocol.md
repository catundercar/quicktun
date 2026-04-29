# Agent ↔ Control Plane 协议

> 网点 quicktun-agent 跟控制面之间的通信协议。也用 gRPC + grpc-gateway，独立 service，独立 token。

## 1. 设计原则

- **声明式下发**：控制面给 agent "你应该跑成什么样" 的描述，agent 自己对齐到该状态
- **agent 主动 pull**：agent 永远主动连控制面，不监听任何端口（最小攻击面）
- **配置版本号**：每次配置变更控制面 bump 版本，agent 拿到新版本号时才重新拉
- **token 与 site 一对一**：agent 用 site_agent_token 认证，token 颁发后只在跨 join 时旋转一次

## 2. 端点（gRPC）

```proto
syntax = "proto3";

package quicktun.agent.v1;

import "google/api/annotations.proto";
import "google/protobuf/timestamp.proto";

service AgentService {
  // 首次注册：用 join-token 换长期 site_agent_token
  rpc Register(RegisterRequest) returns (RegisterResponse) {
    option (google.api.http) = {
      post: "/agent/v1:register"
      body: "*"
    };
  }

  // 周期心跳；返回当前 config 版本号
  rpc Heartbeat(HeartbeatRequest) returns (HeartbeatResponse) {
    option (google.api.http) = {
      post: "/agent/v1:heartbeat"
      body: "*"
    };
  }

  // 拉取下发配置
  rpc GetConfig(GetConfigRequest) returns (GetConfigResponse) {
    option (google.api.http) = {
      get: "/agent/v1/config"
    };
  }

  // 上报事件（连接成功/失败、配置应用结果等）
  rpc ReportEvent(ReportEventRequest) returns (ReportEventResponse) {
    option (google.api.http) = {
      post: "/agent/v1:reportEvent"
      body: "*"
    };
  }
}
```

## 3. 消息定义

### 3.1 Register

```proto
message RegisterRequest {
  string join_token   = 1; // 控制面预生成的一次性 token（site:install 时给）
  string hostname     = 2;
  string os           = 3; // linux / windows
  string arch         = 4; // amd64 / arm64
  string agent_version = 5;
  NetworkInfo network = 6;
}

message NetworkInfo {
  bool   udp_egress     = 1; // 出站 UDP 是否可达控制面
  bool   tcp_443_egress = 2; // 出站 TCP/443 是否可达
  string nat_type       = 3; // none / cone / symmetric / unknown
  repeated string lan_cidrs = 4; // 自动探测的本地网段
  string public_ip      = 5; // 控制面侧回填
}

message RegisterResponse {
  string site_name        = 1; // projects/{p}/sites/{s}
  string site_agent_token = 2; // 长期 token，覆盖 join-token
  google.protobuf.Timestamp expire_time = 3; // null = 不过期
  int64  config_version   = 4;
}
```

### 3.2 Heartbeat

```proto
message HeartbeatRequest {
  // Authorization: Bearer <site_agent_token>
  AgentRuntimeStatus status = 1;
  int64 current_config_version = 2;
}

message AgentRuntimeStatus {
  bool   rathole_client_running = 1;
  string rathole_client_state   = 2; // connected / reconnecting / failed
  google.protobuf.Timestamp started_at = 3;
  uint64 sent_bytes     = 4;
  uint64 received_bytes = 5;
  string last_error     = 6;
  AgentResources resources = 7;
}

message AgentResources {
  uint64 mem_rss_bytes  = 1;
  double cpu_percent    = 2;
  uint32 open_fd_count  = 3;
}

message HeartbeatResponse {
  int64 latest_config_version = 1; // != client 的就拉新配置
  google.protobuf.Duration next_heartbeat_in = 2; // 默认 30s，控制面可调
  repeated AgentCommand commands = 3; // 控制面下发的非常规指令
}

message AgentCommand {
  string id = 1;
  oneof command {
    CmdRestart restart = 10;
    CmdRefreshConfig refresh = 11;
    CmdRunDiagnostic diagnostic = 12;
  }
}

message CmdRestart {}
message CmdRefreshConfig {}
message CmdRunDiagnostic { string kind = 1; }
```

### 3.3 GetConfig

```proto
message GetConfigRequest {
  int64 known_version = 1; // 客户端已知版本，控制面据此决定增量/全量
}

message GetConfigResponse {
  int64 version = 1;
  AgentConfig config = 2;
}

message AgentConfig {
  string backend = 1; // 'rathole'

  oneof backend_config {
    RatholeClientConfig rathole = 10;
    // NetbirdClientConfig netbird = 11; // Phase 2
  }

  repeated ServiceMapping services = 20;
  ControlPlaneConfig control_plane = 30;
}

message RatholeClientConfig {
  string remote_addr     = 1; // relay.example.com:443
  string client_token    = 2; // rathole 自己的 token
  string transport       = 3; // tls
  string sni             = 4;
}

message ServiceMapping {
  string name        = 1;
  string target_addr = 2;
  uint32 target_port = 3;
  uint32 relay_port  = 4; // rathole 在 relay 侧绑定的端口（127.0.0.1:relay_port）
  string proto       = 5;
}

message ControlPlaneConfig {
  string control_plane_url = 1;
  google.protobuf.Duration heartbeat_interval = 2;
  google.protobuf.Duration config_poll_interval = 3;
}
```

### 3.4 ReportEvent

```proto
message ReportEventRequest {
  repeated AgentEvent events = 1;
}

message AgentEvent {
  google.protobuf.Timestamp time = 1;
  string kind = 2; // rathole_started / rathole_failed / config_applied / etc.
  string message = 3;
  google.protobuf.Struct extra = 4;
}

message ReportEventResponse {}
```

## 4. 鉴权

| 阶段 | 凭据 | 来源 |
|---|---|---|
| Register | `join_token`（一次性） | 控制面 `SiteService.GetSiteInstallCommand` 颁发，5min 有效 |
| Heartbeat / GetConfig / ReportEvent | `site_agent_token`（长期） | Register 后下发 |

token 通过 metadata 传：

```
authorization: Bearer <token>
```

控制面拦截器统一校验：

```go
func AgentAuthInterceptor(store *agent.Store) grpc.UnaryServerInterceptor {
    return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, h grpc.UnaryHandler) (any, error) {
        // Register 走 join_token 字段，不在 metadata
        if info.FullMethod == "/quicktun.agent.v1.AgentService/Register" {
            return h(ctx, req)
        }
        token := extractBearer(ctx)
        site, err := store.ValidateAgentToken(ctx, token)
        if err != nil { return nil, status.Error(codes.Unauthenticated, "invalid agent token") }
        // 更新 last_seen / last_used
        return h(context.WithValue(ctx, ctxKeySite, site), req)
    }
}
```

## 5. Agent 端状态机

```
                  ┌──────────────┐
                  │   STARTING   │
                  └──────┬───────┘
                         │ 装机 / 启动
                         ▼
                  ┌──────────────┐  /agent/v1:register
                  │ REGISTERING  │ ──────────────────────► 控制面
                  └──────┬───────┘
                         │ token+config
                         ▼
                  ┌──────────────┐
       ┌──────────│   SYNCING    │ ── GetConfig ──► 控制面
       │          └──────┬───────┘
       │                 │ apply
       │                 ▼
       │          ┌──────────────┐
       │          │   RUNNING    │ ── Heartbeat 30s ──► 控制面
       │          └──────┬───────┘
       │                 │ config_version 变化
       │                 ▼
       │          ┌──────────────┐
       └──────────│  RELOADING   │ ── kill rathole-client + restart with new config
                  └──────────────┘
```

## 6. Agent 端进程管理

agent 自己也用 supervisor 模式管 rathole-client 子进程：

```go
type RatholeClientSupervisor struct {
    config  RatholeClientConfig
    cmd     *exec.Cmd
}

// 跟控制面 supervisor 一样的模式：Pdeathsig + 指数退避重启 + 日志走 zap
```

详细见 [05-process-supervisor.md](./05-process-supervisor.md)。

## 7. 平台抽象

agent 内部分 `Platform` interface：

```go
type Platform interface {
    EnsureUser(name string) error              // 创建 qt-ops 用户
    InstallService(spec ServiceSpec) error     // 注册成 systemd / Windows Service
    PathFor(kind PathKind) string              // 配置 / 日志 / 二进制路径
    SetFirewallAllow(port uint16) error
    KillProcessGroup(pid int) error            // Linux Pdeathsig vs Windows Job Object
}

// implementations: linuxPlatform / windowsPlatform
```

## 8. 错误处理

- **Register 失败**（join_token 无效/过期）→ agent 退出，让 install.sh 提示用户重新生成
- **Heartbeat 失败**（网络抖动）→ 指数退避重试，不影响 rathole-client 子进程运行
- **GetConfig 失败** → 沿用上一份已落盘配置，下次 heartbeat 再试
- **rathole-client 频繁崩溃**（>10 次/分钟）→ 上报 `rathole_unstable` 事件，控制面在 UI 标红

## 9. 抓包友好

heartbeat 和 config 都走 gRPC + JSON gateway，agent 实现走原生 gRPC。OpenAPI/JSON 可用来：

- 排障（curl 模拟 agent）
- 第三方监控集成（Prometheus exporter 抓 site 状态）

## 10. Phase 1 不实现的

- ❌ Long-lived stream（agent 用 polling 心跳，不开 server-streaming）— Phase 2 改 BiDi stream 推命令
- ❌ Agent 自更新（手动重装）
- ❌ 配置回滚机制（控制面下发即生效，错了就再改）
- ❌ Agent 间 P2P（只 agent ↔ 控制面）
