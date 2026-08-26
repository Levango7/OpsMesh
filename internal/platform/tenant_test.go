// tenant_test.go platform 租户数据模型保留测试。
//
// 历史：原 TenantManager 测试（TestValidateTenant*/TestCheckQuota*）已随 H7
// 平台死代码清理删除——TenantManager 类型已移除，校验/配额逻辑改由 controlplane
// handler 侧就近实现并测试。本文件保留 package 声明以维持编译完整性。
package platform
