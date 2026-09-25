module github.com/Levango7/OpsMesh/services/runbook-svc

go 1.26.0

toolchain go1.26.6

require github.com/google/uuid v1.6.0

require github.com/Levango7/OpsMesh v0.0.0-00010101000000-000000000000

// 本地 workspace 替换：不联网解析版本，构建期即根模块源码。
replace github.com/Levango7/OpsMesh => ../../
