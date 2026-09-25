module github.com/Levango7/OpsMesh/services/incident-svc

go 1.26.0

toolchain go1.26.6

require (
	github.com/go-sql-driver/mysql v1.10.0
	github.com/google/uuid v1.6.0
)

require filippo.io/edwards25519 v1.2.0 // indirect

require github.com/Levango7/OpsMesh v0.0.0-00010101000000-000000000000

// 本地 workspace 替换：不联网解析版本，构建期即根模块源码。
replace github.com/Levango7/OpsMesh => ../../
