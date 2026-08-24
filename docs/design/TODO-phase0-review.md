# Phase 0 审查 TODO 清单

> 审查日期：2026-08-25
> 审查范围：Phase 0 地基建设（P0.1-P0.4）
> 状态：已记录为 TODO，不阻塞 Phase 1 进度，后续迭代修复

## 代码 High（4个）

### C-1: log_collect.go logCollectMaxRecords 截断丢日志
- **文件**: `internal/agent/log_collect.go:329-332, 398-401`
- **问题**: `collectFile` 在解析行之前就把 `offsets[file] = offset + len(data)` 推进到末尾；循环中 `len(records) >= 1000` 时 break，剩余行被永久跳过
- **影响**: 短行场景（100B/行）可丢 ~900KB 日志
- **修复**: break 时回退 offset 到已处理行的末尾字节偏移

### C-2: Store 新接口返回内部指针无深拷贝
- **文件**: `internal/store/memory_discovery.go, memory_config.go, memory_secret.go`
- **问题**: Get/Set 方法直接返回/存储入参指针，无深拷贝
- **影响**: 调用方修改返回值破坏 store 内部状态，并发下 data race
- **修复**: 所有读路径返回前值拷贝（`v := *item; return &v`），所有写路径存入前值拷贝

### C-3: Store 新接口零测试覆盖
- **文件**: 缺失 `memory_discovery_test.go, memory_config_test.go, memory_secret_test.go`
- **问题**: ServiceDiscoveryStore/ConfigStore/SecretStore 无任何单元测试
- **修复**: 新增测试覆盖 CRUD + 版本递增 + 历史截断 + 租户隔离 + 并发（-race）

### C-4: log_collect.go sentinel error 与 errors.Is 不兼容
- **文件**: `internal/agent/log_collect.go:532-557`
- **问题**: `wrap` 返回新实例，`errors.Is(err, errLogCollectRuleCompile)` 永远 false
- **修复**: 实现 `Is(target error) bool` 方法，或改用 `fmt.Errorf("%w: %v", sentinel, err)`

## 设计文档 High（3个）

### D-1: 02 vs 07 Fluent Bit 去留矛盾
- **文件**: `docs/design/02-agent-v2-design.md` vs `docs/design/07-log-backend-strategy.md`
- **问题**: 02 说 Agent v2.0 内置 LogCollector 替代 Fluent Bit，07 仍用 Fluent Bit 做路由
- **修复**: 统一日志链路 — Agent LogCollector 直接推 Loki/ES（按租户路由），07 的路由决策下沉到 Agent 配置

### D-2: 05 Vault rotation 配置技术性错误
- **文件**: `docs/design/05-secret-management.md`
- **问题**: KV v2 data 里声明 `rotation: "24h"` 无效，Vault KV v2 不自动轮转
- **修复**: 改用 Vault database secrets engine + rotation role

### D-3: 01 与现有 auth.go JWT 模式过渡缺失
- **文件**: `docs/design/01-auth-architecture.md`
- **问题**: 现有 HS256 自签发+双Cookie 与 Keycloak RS256 验签的过渡路径未设计
- **修复**: 补充 Phase 2.5"双模式并存期"：验签先尝试 RS256（Keycloak）失败再回退 HS256

## 代码 Medium（8个）— 后续迭代

- M-1: `allowRate` 注释说"滑动窗口"但实为固定窗口 → 改注释或用 token bucket
- M-2: `expandPaths` 静默吞掉 glob 语法错误 → err != nil 时 logx.Warn
- M-3: `FireHook` 的 ctx 参数被 `_` 丢弃 → ev.Ctx == nil 时用入参 ctx 兜底
- M-4: `Register` 占坑后 Init 期间可拿到"半注册"插件 → 增加 initialized 标志
- M-5: `sortServiceInstances` 用 O(n²) 插入排序 → 统一用 sort.Slice
- M-6: `SetConfig`/`SetSecret` 不校验 Key 为空 → Key == "" 时 return nil
- M-7: `RotateSecret` 先 RLock 再 SetSecret(Lock) 非原子 → 合并进写锁
- M-8: `multi_schema_p03.go` 修改入参 TenantID → 路由层不修改入参

## 设计文档 Medium（13个）— 后续迭代

- DM-1: 00 迁移路径 Phase 1 同阶段过激 → 拆为 1a/1b/1c 渐进切流
- DM-2: 01 数据流图不符合 OIDC 授权码流 → 改为授权码流时序图
- DM-3: 01 public_key 静态文件无法应对密钥轮转 → 改为 JWKS endpoint 动态拉取
- DM-4: 02 配置字段名与代码不对齐 → 统一为 include_rules/exclude_rules
- DM-5: 03 数据流图未区分注册流/查询流 → 拆为两条线
- DM-6: 04 现有 deploy handler 角色变更未说明 → 补充 deprecated 处置策略
- DM-7: 05 SQL/Memory SecretStore 与 Vault 降级关系未说明 → 补充降级策略
- DM-8: 06 Zabbix 凭据注入未串联 05 → 明确经 ESO 从 Vault 注入
- DM-9: 06 alertengine 与 Alertmanager 关系未明 → 说明替换或共存
- DM-10: 07 backend:"auto" 决策逻辑未定义 → 明确按租户/数据量决策
- DM-11: 08 Waypoint 和 Sidecar 混在同一示例 → 拆为两个独立示例
- DM-12: 09 Tekton API 用 v1beta1 → 升级到 v1
- DM-13: 09 迁移工具适用边界未明确 → 说明仅处理声明式 pipeline