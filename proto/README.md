# M3-3A protobuf 引入：proto/ 目录

本目录定义 agent↔控制面 注册通道的 protobuf IDL 契约，配合 `internal/grpcx/pb/` 生成的 Go stub，
作为手写 `grpc.ServiceDesc` + JSON codec 路径的**并行替代**（兼容期两条路径并存，灰度切换）。

## 目录结构

```
proto/
├── buf.yaml              # buf 模块配置（lint=STANDARD, breaking=FILE）
├── buf.gen.yaml          # buf 代码生成配置（go + go-grpc 插件）
├── opsmesh/v1/
│   └── registration.proto  # Registration 服务 + 12 个消息定义
└── scripts/
    └── gen.sh            # Docker buf 生成脚本（无需本机装 buf/protoc）
```

## 生成 Go stub

```bash
bash proto/scripts/gen.sh
```

生成到 `internal/grpcx/pb/`：
- `registration.pb.go`：消息结构体（`AgentInfo`/`Task`/`TaskResult`/...）
- `registration_grpc.pb.go`：`RegistrationServer`/`RegistrationClient` 接口 + `Registration_ServiceDesc`

生成结果**提交到仓库**，避免无 Docker 的开发者重新生成（沙箱无 Docker 时直接用已提交的 stub）。

## 兼容期切换

| 路径 | ServiceDesc | codec | 默认 |
|------|-------------|-------|------|
| 手写（旧） | `grpcx.Registration_ServiceDesc` | `grpcx.JSONCodec` | ✅ 当前 |
| 生成（新） | `pbv1.Registration_ServiceDesc` | protobuf（默认） | 灰度中 |

切换方式：在 `controlplane/server.go` 把
```go
gs.RegisterService(&grpcx.Registration_ServiceDesc, &grpcServerImpl{...})
```
改为
```go
gs.RegisterService(&pbv1.Registration_ServiceDesc, grpcx.NewStubAdapter(&grpcServerImpl{...}))
```
（`grpcx.StubAdapter` 把现有 `grpcx.RegistrationServer` 实现桥接到生成的 `pbv1.RegistrationServer` 接口）。

兼容期保留手写路径，**不删除** `internal/grpcx/service.go`/`codec.go`，待生成 stub 全量验证后移除。

## CI 守护

- `proto-lint`：`buf lint`（STANDARD 规则集）
- `proto-breaking`：`buf breaking --against '.git#branch=main'`（FILE 策略，禁止删字段/改类型/改字段号）

## 与 internal/proto 的关系

`internal/proto/model.go` 是 JSON 友好的 Go 结构体（手写，被 store/handler/agent 大量引用），
**本目录不替代它**。proto 消息是独立的 IDL，由 `internal/grpcx/stub.go` 的互转函数
（`AgentInfoToProto`/`AgentInfoLegacy`/`TaskToProto`/`TaskLegacy`/...）在传输层与 `internal/proto` 互转。
未来若全量切到 protobuf，可让 `internal/proto` 直接成为 `pbv1` 的类型别名（兼容期不做）。