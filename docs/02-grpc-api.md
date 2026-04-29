# gRPC + grpc-gateway 接口设计

> 严格遵循 [Google API Design Guide](https://cloud.google.com/apis/design)，资源导向 + 标准方法。

## 1. 总体原则

### 1.1 资源导向（Resource-Oriented Design）

API 设计围绕**资源**而不是动作（Resource over RPC）。资源用 URI / resource name 标识，操作用**标准方法**：`List` / `Get` / `Create` / `Update` / `Delete`，必要时加自定义方法。

### 1.2 资源命名

```
projects/{project}                                   # Project
projects/{project}/sites/{site}                      # Site (project-scoped)
projects/{project}/sites/{site}/services/{service}   # Service (site-scoped)
operators/{operator}                                 # Operator (global)
operators/{operator}/sessions/{session}              # Session
```

- `{project}` 等花括号是资源 ID 占位符，使用 **slug 字符串**而不是数字 ID
- 资源类型用复数（projects、sites、services）
- 子资源通过路径嵌套表达从属关系

### 1.3 Proto 包与目录

```
api/
├── quicktun/
│   ├── v1/
│   │   ├── project.proto       # Project 资源
│   │   ├── site.proto          # Site 资源
│   │   ├── service.proto       # Service 资源
│   │   ├── operator.proto      # Operator + Session
│   │   ├── auth.proto          # Login / Logout / Refresh
│   │   ├── audit.proto         # Audit log
│   │   └── common.proto        # 通用类型（Pagination, Empty, Status）
│   └── agent/
│       └── v1/
│           ├── agent.proto     # Site agent ↔ control plane 协议
│           └── config.proto    # 下发配置定义
```

`v1` 是 API 版本，**永远不会破坏性变更**，破坏性变更直接出 `v2`。

## 2. 标准方法映射（gRPC ↔ HTTP）

| 操作 | gRPC method | HTTP verb | URL 模式 |
|---|---|---|---|
| 列表 | `ListXxx` | GET | `/v1/{parent=projects/*}/sites` |
| 获取 | `GetXxx` | GET | `/v1/{name=projects/*/sites/*}` |
| 创建 | `CreateXxx` | POST | `/v1/{parent=projects/*}/sites` |
| 更新 | `UpdateXxx` | PATCH | `/v1/{site.name=projects/*/sites/*}` |
| 删除 | `DeleteXxx` | DELETE | `/v1/{name=projects/*/sites/*}` |
| 自定义 | `XxxAction` | POST | `/v1/{name=projects/*/sites/*}:action` |

自定义动作用**冒号** + 动词（`:rotate`、`:enable`、`:test`）。

## 3. 通用类型 (common.proto)

```proto
syntax = "proto3";

package quicktun.v1;

import "google/protobuf/timestamp.proto";

option go_package = "github.com/<org>/quicktun/api/quicktun/v1;quicktunv1";

// PageRequest 标准分页请求字段（Google AIP-158）
message PageRequest {
  int32 page_size  = 1;  // 每页大小，默认 50，最大 1000
  string page_token = 2;  // 上一次返回的 next_page_token
}

// PageResponse 标准分页返回字段
message PageResponse {
  string next_page_token = 1;
  int32 total_size = 2;  // 可选；高代价时省略
}

// LongRunningOperation Phase 2 用，预留
message Operation {
  string name = 1;
  bool done = 2;
  google.protobuf.Timestamp create_time = 3;
}
```

## 4. Project API (project.proto)

```proto
syntax = "proto3";

package quicktun.v1;

import "google/api/annotations.proto";
import "google/api/field_behavior.proto";
import "google/protobuf/field_mask.proto";
import "google/protobuf/empty.proto";
import "google/protobuf/timestamp.proto";
import "quicktun/v1/common.proto";

service ProjectService {
  rpc ListProjects(ListProjectsRequest) returns (ListProjectsResponse) {
    option (google.api.http) = {
      get: "/v1/projects"
    };
  }

  rpc GetProject(GetProjectRequest) returns (Project) {
    option (google.api.http) = {
      get: "/v1/{name=projects/*}"
    };
  }

  rpc CreateProject(CreateProjectRequest) returns (Project) {
    option (google.api.http) = {
      post: "/v1/projects"
      body: "project"
    };
  }

  rpc UpdateProject(UpdateProjectRequest) returns (Project) {
    option (google.api.http) = {
      patch: "/v1/{project.name=projects/*}"
      body: "project"
    };
  }

  rpc DeleteProject(DeleteProjectRequest) returns (google.protobuf.Empty) {
    option (google.api.http) = {
      delete: "/v1/{name=projects/*}"
    };
  }
}

message Project {
  // Resource name. Format: projects/{project}
  string name = 1;

  // Output only.
  string project_id = 2 [(google.api.field_behavior) = OUTPUT_ONLY]; // 数字 ID
  google.protobuf.Timestamp create_time = 3 [(google.api.field_behavior) = OUTPUT_ONLY];
  google.protobuf.Timestamp update_time = 4 [(google.api.field_behavior) = OUTPUT_ONLY];

  // Mutable.
  string display_name      = 10 [(google.api.field_behavior) = REQUIRED];
  SiteMode default_mode    = 11;  // ENDPOINT / SUBNET
  Backend  backend         = 12;  // RATHOLE / NETBIRD
  string   relay_port_range = 13 [(google.api.field_behavior) = REQUIRED]; // "20000-20999"
  ProjectStatus status     = 14;  // ACTIVE / DISABLED
}

enum SiteMode {
  SITE_MODE_UNSPECIFIED = 0;
  SITE_MODE_ENDPOINT    = 1;
  SITE_MODE_SUBNET      = 2;  // Phase 2
}

enum Backend {
  BACKEND_UNSPECIFIED = 0;
  BACKEND_RATHOLE     = 1;
  BACKEND_NETBIRD     = 2;  // Phase 2
}

enum ProjectStatus {
  PROJECT_STATUS_UNSPECIFIED = 0;
  PROJECT_STATUS_ACTIVE      = 1;
  PROJECT_STATUS_DISABLED    = 2;
}

message ListProjectsRequest {
  PageRequest page = 1;
  string filter  = 2;  // AIP-160 filter expression（Phase 2）
  string order_by = 3; // AIP-132 排序（Phase 2）
}

message ListProjectsResponse {
  repeated Project projects = 1;
  PageResponse page = 2;
}

message GetProjectRequest {
  string name = 1 [(google.api.field_behavior) = REQUIRED];
}

message CreateProjectRequest {
  string project_id = 1 [(google.api.field_behavior) = REQUIRED]; // slug，URL-safe
  Project project = 2 [(google.api.field_behavior) = REQUIRED];
}

message UpdateProjectRequest {
  Project project = 1 [(google.api.field_behavior) = REQUIRED];
  google.protobuf.FieldMask update_mask = 2 [(google.api.field_behavior) = REQUIRED];
}

message DeleteProjectRequest {
  string name = 1 [(google.api.field_behavior) = REQUIRED];
  bool force = 2; // 默认不允许删除还有 site 的 project
}
```

**关键点说明（Google API 规范）：**

- `name` 字段：永远是资源 full name (`projects/{project}`)，不是裸 ID
- `project_id` 在 Create 时单独传：客户端选定的 slug
- `create_time` / `update_time`：用 `Timestamp`，标 `OUTPUT_ONLY`
- `field_behavior` annotation：标记 `REQUIRED` / `OUTPUT_ONLY` / `IMMUTABLE`，配合 lint 工具校验
- `UpdateXxx` 必须用 `FieldMask` 表达"更新哪些字段"，不允许全量替换
- enum：`UNSPECIFIED` 必须是 0，避免默认值歧义

## 5. Site API (site.proto)

```proto
syntax = "proto3";

package quicktun.v1;

import "google/api/annotations.proto";
import "google/api/field_behavior.proto";
import "google/protobuf/field_mask.proto";
import "google/protobuf/empty.proto";
import "google/protobuf/timestamp.proto";
import "quicktun/v1/common.proto";
import "quicktun/v1/project.proto";

service SiteService {
  rpc ListSites(ListSitesRequest) returns (ListSitesResponse) {
    option (google.api.http) = {
      get: "/v1/{parent=projects/*}/sites"
    };
  }

  rpc GetSite(GetSiteRequest) returns (Site) {
    option (google.api.http) = {
      get: "/v1/{name=projects/*/sites/*}"
    };
  }

  rpc CreateSite(CreateSiteRequest) returns (Site) {
    option (google.api.http) = {
      post: "/v1/{parent=projects/*}/sites"
      body: "site"
    };
  }

  rpc UpdateSite(UpdateSiteRequest) returns (Site) {
    option (google.api.http) = {
      patch: "/v1/{site.name=projects/*/sites/*}"
      body: "site"
    };
  }

  rpc DeleteSite(DeleteSiteRequest) returns (google.protobuf.Empty) {
    option (google.api.http) = {
      delete: "/v1/{name=projects/*/sites/*}"
    };
  }

  // Custom method: 重置 agent token（rotate）
  rpc RotateSiteAgentToken(RotateSiteAgentTokenRequest) returns (RotateSiteAgentTokenResponse) {
    option (google.api.http) = {
      post: "/v1/{name=projects/*/sites/*}:rotateAgentToken"
      body: "*"
    };
  }

  // Custom method: 生成安装命令
  rpc GetSiteInstallCommand(GetSiteInstallCommandRequest) returns (GetSiteInstallCommandResponse) {
    option (google.api.http) = {
      get: "/v1/{name=projects/*/sites/*}:installCommand"
    };
  }
}

message Site {
  string name = 1; // projects/{project}/sites/{site}

  string site_id = 2 [(google.api.field_behavior) = OUTPUT_ONLY];
  google.protobuf.Timestamp create_time = 3 [(google.api.field_behavior) = OUTPUT_ONLY];
  google.protobuf.Timestamp update_time = 4 [(google.api.field_behavior) = OUTPUT_ONLY];

  string display_name = 10 [(google.api.field_behavior) = REQUIRED];
  SiteMode mode       = 11;  // 默认从 project 继承
  Backend backend     = 12;  // 默认从 project 继承
  repeated string lan_cidrs = 13;  // ["192.168.10.0/24"]

  // Output only.
  SiteStatus status   = 20 [(google.api.field_behavior) = OUTPUT_ONLY];
  google.protobuf.Timestamp last_seen_time = 21 [(google.api.field_behavior) = OUTPUT_ONLY];
  string hostname     = 22 [(google.api.field_behavior) = OUTPUT_ONLY];
  string os           = 23 [(google.api.field_behavior) = OUTPUT_ONLY];
  string agent_version = 24 [(google.api.field_behavior) = OUTPUT_ONLY];
}

enum SiteStatus {
  SITE_STATUS_UNSPECIFIED = 0;
  SITE_STATUS_PENDING     = 1;
  SITE_STATUS_ONLINE      = 2;
  SITE_STATUS_OFFLINE     = 3;
}

message ListSitesRequest {
  string parent = 1 [(google.api.field_behavior) = REQUIRED]; // projects/{project}
  PageRequest page = 2;
  string filter = 3;
}

message ListSitesResponse {
  repeated Site sites = 1;
  PageResponse page = 2;
}

message GetSiteRequest { string name = 1 [(google.api.field_behavior) = REQUIRED]; }

message CreateSiteRequest {
  string parent = 1 [(google.api.field_behavior) = REQUIRED];
  string site_id = 2 [(google.api.field_behavior) = REQUIRED]; // slug
  Site site = 3 [(google.api.field_behavior) = REQUIRED];
}

message UpdateSiteRequest {
  Site site = 1 [(google.api.field_behavior) = REQUIRED];
  google.protobuf.FieldMask update_mask = 2 [(google.api.field_behavior) = REQUIRED];
}

message DeleteSiteRequest {
  string name = 1 [(google.api.field_behavior) = REQUIRED];
  bool force = 2; // 默认不允许删除还有 service 的 site
}

message RotateSiteAgentTokenRequest {
  string name = 1 [(google.api.field_behavior) = REQUIRED];
}

message RotateSiteAgentTokenResponse {
  string token = 1; // 一次性返回，后续不可查
  google.protobuf.Timestamp expire_time = 2;
}

message GetSiteInstallCommandRequest {
  string name = 1 [(google.api.field_behavior) = REQUIRED];
  string os = 2;  // linux / windows，默认 linux
}

message GetSiteInstallCommandResponse {
  string command = 1;       // curl ... | bash 或 PowerShell 等价物
  string token = 2;         // 嵌入命令的 join-token
  google.protobuf.Timestamp expire_time = 3;
}
```

## 6. Service API (service.proto)

```proto
service ServiceService {
  rpc ListServices(ListServicesRequest) returns (ListServicesResponse) {
    option (google.api.http) = {
      get: "/v1/{parent=projects/*/sites/*}/services"
    };
  }

  rpc GetService(GetServiceRequest) returns (Service) {
    option (google.api.http) = {
      get: "/v1/{name=projects/*/sites/*/services/*}"
    };
  }

  rpc CreateService(CreateServiceRequest) returns (Service) {
    option (google.api.http) = {
      post: "/v1/{parent=projects/*/sites/*}/services"
      body: "service"
    };
  }

  rpc UpdateService(UpdateServiceRequest) returns (Service) {
    option (google.api.http) = {
      patch: "/v1/{service.name=projects/*/sites/*/services/*}"
      body: "service"
    };
  }

  rpc DeleteService(DeleteServiceRequest) returns (google.protobuf.Empty) {
    option (google.api.http) = {
      delete: "/v1/{name=projects/*/sites/*/services/*}"
    };
  }
}

message Service {
  string name = 1; // projects/{p}/sites/{s}/services/{svc}

  string service_id = 2 [(google.api.field_behavior) = OUTPUT_ONLY];
  google.protobuf.Timestamp create_time = 3 [(google.api.field_behavior) = OUTPUT_ONLY];
  google.protobuf.Timestamp update_time = 4 [(google.api.field_behavior) = OUTPUT_ONLY];

  string display_name = 10 [(google.api.field_behavior) = REQUIRED];
  string target_addr  = 11 [(google.api.field_behavior) = REQUIRED]; // 127.0.0.1 or LAN IP
  uint32 target_port  = 12 [(google.api.field_behavior) = REQUIRED]; // proto3 没 uint16，用 uint32 + 校验
  Proto  proto        = 13;

  // Output only.
  uint32 relay_port = 20 [(google.api.field_behavior) = OUTPUT_ONLY]; // 控制面分配
}

enum Proto {
  PROTO_UNSPECIFIED = 0;
  PROTO_TCP         = 1;
  PROTO_UDP         = 2; // Phase 2
}
```

## 7. Auth API (auth.proto)

```proto
service AuthService {
  rpc Login(LoginRequest) returns (LoginResponse) {
    option (google.api.http) = {
      post: "/v1/auth:login"
      body: "*"
    };
  }

  rpc Logout(LogoutRequest) returns (google.protobuf.Empty) {
    option (google.api.http) = {
      post: "/v1/auth:logout"
      body: "*"
    };
  }

  // 用 refresh token 换新 access token，Phase 2
  rpc RefreshToken(RefreshTokenRequest) returns (RefreshTokenResponse) {
    option (google.api.http) = {
      post: "/v1/auth:refresh"
      body: "*"
    };
  }

  rpc WhoAmI(google.protobuf.Empty) returns (WhoAmIResponse) {
    option (google.api.http) = {
      get: "/v1/auth:whoami"
    };
  }
}

message LoginRequest {
  string email    = 1 [(google.api.field_behavior) = REQUIRED];
  string password = 2 [(google.api.field_behavior) = REQUIRED];
}

message LoginResponse {
  string access_token = 1;
  google.protobuf.Timestamp expire_time = 2;
  Operator operator = 3;
}

message LogoutRequest {
  // 不传 = logout 当前 session；传 session_name = 撤销指定 session（admin）
  string session_name = 1; // operators/{op}/sessions/{sid}
}

message WhoAmIResponse {
  Operator operator = 1;
  repeated Project accessible_projects = 2;
}
```

## 8. Audit API (audit.proto)

```proto
service AuditService {
  rpc ListAuditLogs(ListAuditLogsRequest) returns (ListAuditLogsResponse) {
    option (google.api.http) = {
      get: "/v1/{parent=projects/*}/auditLogs"
      additional_bindings {
        get: "/v1/auditLogs"  // admin 跨项目
      }
    };
  }
}

message AuditLog {
  string name = 1; // projects/{p}/auditLogs/{id} or auditLogs/{id}
  google.protobuf.Timestamp time = 2;
  string operator   = 3; // operators/{op}
  string action     = 4;
  string target     = 5;
  string source_ip  = 6;
  google.protobuf.Struct extra = 7;
}
```

## 9. 错误模型

遵循 [google.rpc.Status](https://github.com/googleapis/googleapis/blob/master/google/rpc/status.proto)：

```proto
import "google/rpc/error_details.proto";
```

| gRPC code | HTTP | 何时用 |
|---|---|---|
| `INVALID_ARGUMENT` | 400 | 字段格式错 |
| `UNAUTHENTICATED` | 401 | token 缺失/无效 |
| `PERMISSION_DENIED` | 403 | 无 project 访问权 |
| `NOT_FOUND` | 404 | 资源不存在 |
| `ALREADY_EXISTS` | 409 | slug 冲突 |
| `FAILED_PRECONDITION` | 400 | 状态前置不满足（如 site 在线时不能删） |
| `RESOURCE_EXHAUSTED` | 429 | 端口段用尽 |
| `INTERNAL` | 500 | 服务端 bug |
| `UNAVAILABLE` | 503 | rathole 拉不起来 |

详情用 `google.rpc.ErrorInfo` / `BadRequest` / `QuotaFailure`：

```go
status.New(codes.InvalidArgument, "invalid site name").
    WithDetails(&errdetails.BadRequest{
        FieldViolations: []*errdetails.BadRequest_FieldViolation{
            {Field: "site_id", Description: "must match [a-z0-9-]{3,32}"},
        },
    })
```

## 10. 鉴权 / 拦截器

### 10.1 协议
- 客户端在 `Authorization: Bearer <token>` 头里带 access token（gRPC metadata key `authorization`）
- grpc-gateway 自动透传 HTTP `Authorization` 到 gRPC metadata

### 10.2 拦截器（Go 服务端）

```go
// pkg/server/interceptor/auth.go
func AuthUnaryInterceptor(authStore *auth.Store) grpc.UnaryServerInterceptor {
    return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
        // 白名单：login 不需要 auth
        if info.FullMethod == "/quicktun.v1.AuthService/Login" {
            return handler(ctx, req)
        }
        token := extractBearer(ctx)
        if token == "" {
            return nil, status.Error(codes.Unauthenticated, "missing token")
        }
        op, err := authStore.Validate(ctx, token)
        if err != nil {
            return nil, status.Error(codes.Unauthenticated, "invalid or expired token")
        }
        ctx = context.WithValue(ctx, ctxKeyOperator, op)
        return handler(ctx, req)
    }
}
```

### 10.3 Project 范围拦截器

后置拦截器解析 request 中的 resource name，提取 `{project}`，校验当前 operator 是否有 access。

## 11. Tooling

### 11.1 buf.gen.yaml

```yaml
version: v2
plugins:
  - remote: buf.build/protocolbuffers/go
    out: gen/go
    opt: paths=source_relative
  - remote: buf.build/grpc/go
    out: gen/go
    opt:
      - paths=source_relative
      - require_unimplemented_servers=false
  - remote: buf.build/grpc-ecosystem/gateway
    out: gen/go
    opt:
      - paths=source_relative
      - generate_unbound_methods=true
  - remote: buf.build/grpc-ecosystem/openapiv2
    out: gen/openapiv2
```

### 11.2 buf.yaml + lint

```yaml
version: v2
modules:
  - path: api
lint:
  use:
    - STANDARD
    - COMMENTS  # 强制注释
  except:
    - PACKAGE_VERSION_SUFFIX  # 已用 v1 在路径里
breaking:
  use:
    - FILE
```

CI 上跑 `buf lint` + `buf breaking --against '.git#branch=main'` 防破坏性变更。

### 11.3 OpenAPI 输出

`buf generate` 同步出 `openapiv2.json`，给 CLI / 文档站点 / 第三方集成。

## 12. CLI 怎么调

`quicktun` CLI 直接调 gRPC（不走 gateway），原因：流式接口（agent supervise）gateway 表达不好；CLI 跟控制面同语言生态。

```go
conn, _ := grpc.Dial("relay.example.com:9443",
    grpc.WithTransportCredentials(creds),
    grpc.WithUnaryInterceptor(tokenAttachInterceptor(token)))
client := quicktunv1.NewSiteServiceClient(conn)
resp, _ := client.ListSites(ctx, &quicktunv1.ListSitesRequest{
    Parent: "projects/clinic-network",
})
```

## 13. 端口规划

| 端口 | 用途 | 协议 |
|---|---|---|
| 9443 | gRPC（mTLS 或 TLS） | HTTP/2 |
| 9080 | grpc-gateway HTTP/JSON | HTTP/1.1 |
| 443  | quicktun-auth-proxy（agent + operator 数据通道） | TLS over TCP |
| 20000-20999 | rathole project A（**仅 127.0.0.1 监听**） | TCP |
| 21000-21999 | rathole project B（**仅 127.0.0.1 监听**） | TCP |

外网只暴露 443（auth-proxy）+ 9443/9080（控制面 API），其他全部 localhost。
