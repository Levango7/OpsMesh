module github.com/Levango7/OpsMesh/services/auth-svc

go 1.26.0

toolchain go1.26.6

require (
	github.com/go-sql-driver/mysql v1.9.0
	github.com/golang-jwt/jwt/v5 v5.3.1
	github.com/google/uuid v1.6.0
	github.com/redis/go-redis/v9 v9.22.0
	golang.org/x/crypto v0.54.0
	google.golang.org/grpc v1.83.0
	google.golang.org/protobuf v1.36.11
	opsmesh v0.0.0-00010101000000-000000000000
)

require (
	filippo.io/edwards25519 v1.1.1 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/dgryski/go-rendezvous v0.0.0-20200823014737-9f7001d12a5f // indirect
	go.uber.org/atomic v1.11.0 // indirect
	golang.org/x/net v0.57.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
	golang.org/x/text v0.40.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260803160001-6ac0973c030d // indirect
)

replace opsmesh => ../../
