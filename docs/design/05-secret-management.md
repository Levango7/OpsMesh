# 05 - 密钥管理分层：Vault + K8s Secret

## 冲突描述

Vault 和 K8s Secret 都想做密钥管理：
- **Vault**：动态密钥、轮转、审计、加密存储
- **K8s Secret**：K8s 原生密钥，Pod 挂载，base64 编码

两者同时管理密钥导致：密钥分散、轮转不一致、安全风险。

## 解决方案

**分层管理**：
- **Vault** → 根密钥库：所有密钥的源头，动态密钥生成、自动轮转、审计日志
- **K8s Secret** → 运行时密钥：由 External Secrets Operator (ESO) 从 Vault 同步
- **OpsMesh SecretStore** → 应用层密钥 API：封装 Vault/K8s Secret 统一查询

**边界规则**：生产密钥 100% 在 Vault，K8s Secret 由 ESO 自动同步，不手动创建。

## 数据流图

```
┌──────────┐    ┌──────────────────┐    ┌──────────────┐    ┌─────┐
│  Vault   │───▶│  External Secrets │───▶│ K8s Secret   │───▶│ Pod │
│ (根密钥) │    │  Operator (ESO)   │    │ (运行时密钥) │    │     │
│          │    │                   │    │              │    │     │
│ 动态密钥  │    │  定期同步         │    │ 自动轮转     │    │ 挂载 │
│ 自动轮转  │    │  回写 K8s         │    │              │    │     │
│ 审计日志  │    │                   │    │              │    │     │
└──────────┘    └──────────────────┘    └──────────────┘    └─────┘
      │
      ▼
┌──────────────────┐
│ OpsMesh Secret   │
│ Store (API 层)   │
│                  │
│ GetSecret()      │
│ RotateSecret()   │
│ ListSecrets()    │
└──────────────────┘
```

## 配置示例

### Vault 密钥定义
```yaml
# Vault KV v2 密钥
path: secret/data/opsmesh/db
data:
  username: "opsmesh"
  password: "<dynamic>"  # Vault 动态生成
  rotation: "24h"         # 每 24 小时轮转
```

### ESO 同步配置
```yaml
apiVersion: external-secrets.io/v1beta1
kind: ExternalSecret
metadata:
  name: db-credentials
spec:
  secretStoreRef:
    name: vault-backend
    kind: SecretStore
  target:
    name: db-credentials  # K8s Secret 名称
  data:
    - secretKey: username
      remoteRef:
        key: opsmesh/db
        property: username
    - secretKey: password
      remoteRef:
        key: opsmesh/db
        property: password
```

## 迁移路径

1. **Phase 1**：部署 Vault + ESO
2. **Phase 2**：现有 K8s Secret 迁移到 Vault
3. **Phase 3**：ESO 自动同步 Vault → K8s Secret
4. **Phase 4**：OpsMesh SecretStore 对接 Vault API
5. **Phase 5**：移除手动创建的 K8s Secret

## 验收标准

- [ ] 生产密钥 100% 在 Vault
- [ ] K8s Secret 由 ESO 自动同步
- [ ] 密钥轮转自动化
- [ ] 审计日志完整
- [ ] OpsMesh SecretStore API 可用

## 风险与缓解

| 风险 | 影响 | 缓解措施 |
|------|------|----------|
| Vault 不可用 | 密钥获取失败 | Vault HA + K8s Secret 缓存 |
| 同步延迟 | Pod 拿到旧密钥 | ESO 轮询间隔 < 轮转间隔 |
| 迁移期间双份密钥 | 不一致 | 灰度迁移 + 原子切换 |