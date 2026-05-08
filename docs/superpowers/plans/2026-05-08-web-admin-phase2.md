# Web Admin Phase 2 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 把 web 从"能看 + 能创建删除"升级到"能审计 + 能改 + 能完整 onboard 新 site + 能管账号"。一次性完成 Linear CAT-17、CAT-18、CAT-19、CAT-20 四个 issue。

**Linear:**
- [CAT-17](https://linear.app/catundercar/issue/CAT-17) Web admin：资源 update 编辑功能
- [CAT-18](https://linear.app/catundercar/issue/CAT-18) Web admin：审计日志查看器
- [CAT-19](https://linear.app/catundercar/issue/CAT-19) Web admin：站点安装命令一键展示
- [CAT-20](https://linear.app/catundercar/issue/CAT-20) Operator CRUD + 项目权限管理

**Out of scope（推到 Phase 3 后续）：** 分页/搜索（CAT-22）、Profile 改密（CAT-23）、Dark mode/i18n（CAT-24）、forward 命令片段（CAT-21）。

---

## 设计决策

### CAT-17 Resource update — 仅前端工作

后端 RPC 已就绪：`UpdateProject` / `UpdateSite` / `UpdateService` 都接受 FieldMask。前端只需：
- 每个资源行加"编辑"按钮 → 弹窗表单
- 表单初始化用现有数据，submit 时构造 FieldMask 仅写入 dirty 字段

可编辑字段：
- **Project**：`display_name`、`relay_port_range`、`status` (active↔disabled)
- **Site**：`display_name`、`mode`
- **Service**：`display_name`、`target_addr`、`target_port`

### CAT-18 Audit log — 新增 AuditService RPC

新增 proto + 后端 + 前端：

```proto
service AuditService {
  rpc ListAuditLogs(ListAuditLogsRequest) returns (ListAuditLogsResponse) {
    option (google.api.http) = { get: "/v1/audit-logs" };
  }
}

message ListAuditLogsRequest {
  uint32 page_size = 1;        // default 50, max 200
  string page_token = 2;       // opaque cursor
  string operator_email = 3;   // optional filter
  string project_slug = 4;     // optional filter
  string action_prefix = 5;    // e.g. "site." matches site.create / site.update / etc.
  google.protobuf.Timestamp since = 6;  // optional
  google.protobuf.Timestamp until = 7;  // optional
}

message ListAuditLogsResponse {
  repeated AuditLogEntry entries = 1;
  string next_page_token = 2;
  uint32 total_size = 3;       // approximate; based on filtered query
}

message AuditLogEntry {
  uint64 id = 1;
  google.protobuf.Timestamp time = 2;
  string operator_email = 3;
  string source_ip = 4;
  string action = 5;           // e.g. "site.create"
  string target = 6;           // e.g. "projects/p1/sites/s1"
  string project_slug = 7;     // for filter convenience
  string extra_json = 8;       // raw JSON
}
```

Admin-only。Web `/audit` 页面：表格 + 过滤器 + cursor 分页（"加载更多"按钮）。

### CAT-19 Install command — 仅前端工作

后端：`SiteService.GetSiteInstallCommand` RPC 已存在（Plan 4），返回一次性 token + URL 模板。前端：

- Site 行加"显示安装命令"按钮 → 弹窗
- 弹窗三个 tab：
  - **Linux**: `curl -fsSL https://raw.githubusercontent.com/.../install-agent.sh | sudo bash -s -- --token <T> --control-endpoint <C>`
  - **macOS**: 同 Linux 脚本（`install-agent.sh` 已支持 Darwin 分支）
  - **Windows (NSSM)**: PowerShell `install-agent.ps1` 命令
  - **Windows (MSI)**: `msiexec /i quicktun-agent.msi /qn` + 编辑 agent.yaml 填入 token
- 警告 banner："此 token 仅显示一次" + 复制按钮
- 关闭后 token 不可恢复，需重新点"显示安装命令"（会调用 RPC 生成新 token，旧的失效）

### CAT-20 Operator CRUD + 项目权限 — 新增 OperatorService RPC

新增 proto + 后端 + CLI + 前端：

```proto
service OperatorService {
  rpc ListOperators(ListOperatorsRequest) returns (ListOperatorsResponse);
  rpc GetOperator(GetOperatorRequest) returns (Operator);
  rpc CreateOperator(CreateOperatorRequest) returns (Operator);
  rpc UpdateOperator(UpdateOperatorRequest) returns (Operator);  // is_admin / display_name
  rpc DeleteOperator(DeleteOperatorRequest) returns (google.protobuf.Empty);

  rpc ListProjectAccess(ListProjectAccessRequest) returns (ListProjectAccessResponse);
  rpc GrantProjectAccess(GrantProjectAccessRequest) returns (OperatorProjectAccess);
  rpc RevokeProjectAccess(RevokeProjectAccessRequest) returns (google.protobuf.Empty);
}
```

所有方法 admin-only（除了 Get 自己）。

**安全护栏：**
- 不能删除自己（基于 ctx 中的 operator id 比对）
- 删除最后一个 admin 时拒绝（COUNT(is_admin=true) - 1 == 0 → FailedPrecondition）
- 改自己的 is_admin 时同样拒绝

CLI 加：`quicktun operator [list|get|create|delete|grant|revoke]`

Web Operators 页面取代当前的 CLI-only banner：
- 列表（admin 看全部、非 admin 看自己 + 公共字段）
- 创建模态框：email + password + is_admin checkbox
- 删除按钮（带 admin 守卫）
- 切换 is_admin（带守卫）
- 详情页（或同页面展开行）：显示该 operator 已授权的项目 + 角色，"添加项目访问"按钮

---

## 文件结构

### 新增

```
api/quicktun/v1/
├── audit.proto              AuditService
├── operator.proto           OperatorService（重写：原文件可能只有 Operator message）

internal/dao/
├── audit.go                 AuditDAO（已有 audit writer，但没有 ListWithFilters；扩展或新建）

internal/grpcsvc/
├── audit_service.go         AuditService 实现
├── audit_service_test.go
├── operator_service.go      OperatorService 实现
├── operator_service_test.go

cmd/quicktun/
├── cmd_operator.go          quicktun operator [...] CLI

web/src/pages/
├── AuditPage.tsx            /audit 页面
├── EditProjectModal.tsx     CAT-17
├── EditSiteModal.tsx        CAT-17
├── EditServiceModal.tsx     CAT-17
├── InstallCommandModal.tsx  CAT-19
└── OperatorDetailPage.tsx   或在 OperatorsPage 内联展开（按实现选择）
```

### 修改

```
internal/server/server.go    注册 AuditService + OperatorService
internal/auth/interceptor.go 必要时调整路径白名单
api/quicktun/v1/operator.proto  替换内容（如已存在）

cmd/quicktun/main.go         注册 newOperatorCmd

web/src/api/types.ts         加 AuditLogEntry / ListAuditLogsResponse / Operator-related 类型
web/src/api/client.ts        无需改（通用 fetch wrapper）
web/src/App.tsx              加 /audit 路由
web/src/layout/nav.ts        加"审计日志"导航项（admin 才显示）
web/src/pages/ProjectsPage.tsx    加"编辑"按钮 + EditProjectModal 集成
web/src/pages/SitesPage.tsx       加"编辑" + "显示安装命令" 两个按钮 + 两个 modal
web/src/pages/ServicesPage.tsx    加"编辑"按钮 + EditServiceModal
web/src/pages/OperatorsPage.tsx   完全重写（替换 CLI banner）
```

---

## 任务拆分（4 个 task，可并行 + 1 个 final）

### T1: AuditService 后端

新增 `audit.proto`、`audit_service.go`、`audit_service_test.go`、扩展 `dao/audit.go`、注册到 server。

**接口**：admin-only ListAuditLogs + 过滤参数。**测试**：filters / 分页 / admin-only。

### T2: OperatorService 后端 + CLI

新增/重写 `operator.proto`（含 Operator + Access RPCs）、`operator_service.go`、`operator_service_test.go`、`cmd_operator.go` CLI、注册到 server。

**安全护栏测试**：禁删自己、禁删最后 admin、禁降自己 is_admin。

### T3: Web admin Phase 2 wave 1（编辑 + 安装命令）

**仅前端**。CAT-17 + CAT-19 合并到一个 implementer 因为 SitesPage.tsx 同时被两个 feature 修改。

- 三个 EditModal 组件 + 集成到三个 list 页面
- InstallCommandModal 集成到 SitesPage
- 用现有 UpdateProject/UpdateSite/UpdateService 和 GetSiteInstallCommand RPC，不需要新 proto

### T4: Web admin Phase 2 wave 2（审计页 + Operator CRUD）

**仅前端**。**依赖 T1 + T2 完成**（需要 `/v1/audit-logs` 和 `/v1/operators` 真实可用）。

- 新增 AuditPage（表格 + 过滤器 + 加载更多）
- 重写 OperatorsPage（列表 + 创建 + 删除 + 切换 admin + 展开"项目权限"面板）

### T5: Final smoke + verify + push

- smoke-cli.sh 加几行 audit + operator 端到端断言
- 全套 gate（unit + race + vet + 4 smoke + lint-deploy + linux/windows 交叉编译）
- 单 commit + push

---

## Wave 编排

**Wave 1**（并行 3 个 implementer，文件不重叠）：
- T1（后端 audit）
- T2（后端 operator + CLI）
- T3（前端 edit + install command）

**Wave 2**（T4 单独，依赖 Wave 1）：
- T4（前端 audit + operator）

**Wave 3**：T5 最终验证 + push

---

## 自检表

| Linear issue | 实现位置 |
|---|---|
| CAT-17（资源 update） | T3（前端） |
| CAT-18（审计日志） | T1（后端 RPC）+ T4（前端） |
| CAT-19（安装命令展示） | T3（前端） |
| CAT-20（Operator CRUD） | T2（后端 + CLI）+ T4（前端） |

四个 issue 在 Wave 3 完成后全部 close。

---

## 推送时机

T5 完成后单次 `git push origin main`。如果 Wave 1 implementers 各自 push 也 OK（自然多 commit），但 T5 必须在所有都 push 完后 pull --rebase 再做最终验证 push。
