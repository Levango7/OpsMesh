package controlplane

// M2-1B Registry 去除：原 Registry 薄间接层（仅对 store.Store 做一对一转发）已删除。
// 消费方（Server / grpcServerImpl / storeDispatcher）现直连 store.Store 小接口组合
// （DeviceStore / TaskStore / AlertStore / AuditStore / TokenStore / LeaderStore）。
// 详见 internal/store/store.go 的接口拆分与 internal/controlplane/server.go 的直连用法。
// 本文件保留 package 声明以避免 git 历史中出现"删除文件"动作，便于 review 与回滚。