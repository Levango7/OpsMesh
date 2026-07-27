module opsmesh

go 1.22

// 外部依赖（沙箱无 Go/无网络，不会执行 go mod tidy）：
// 请在本机执行 `go mod tidy` 拉取并生成 go.sum 后再构建。
// 仅依赖稳定 API；不依赖 protobuf 代码生成（gRPC 走自定义 JSON codec + 手写 ServiceDesc）。
require (
	github.com/go-sql-driver/mysql v1.8.1
	github.com/redis/go-redis/v9 v9.5.1
	github.com/segmentio/kafka-go v0.4.48
	google.golang.org/grpc v1.64.0
)

require golang.org/x/crypto v0.33.0

require (
	filippo.io/edwards25519 v1.1.0 // indirect
	github.com/cespare/xxhash/v2 v2.2.0 // indirect
	github.com/dgryski/go-rendezvous v0.0.0-20200823014737-9f7001d12a5f // indirect
	github.com/klauspost/compress v1.15.9 // indirect
	github.com/pierrec/lz4/v4 v4.1.15 // indirect
	golang.org/x/net v0.22.0 // indirect
	golang.org/x/sys v0.30.0 // indirect
	golang.org/x/text v0.22.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20240318140521-94a12d6c2237 // indirect
	google.golang.org/protobuf v1.33.0 // indirect
)
