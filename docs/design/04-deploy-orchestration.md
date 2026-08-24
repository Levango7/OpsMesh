# 04 - 部署编排分工：ArgoCD + OpsMesh

## 冲突描述

ArgoCD 和 OpsMesh 部署引擎都想做应用部署：
- **ArgoCD**：K8s GitOps 部署，声明式，自动同步
- **OpsMesh 部署引擎**：任务下发、Ansible、脚本部署

两者同时用于 K8s 部署会导致：部署路径不明确、回滚冲突、审计混乱。

## 解决方案

**按目标环境分工**：
- **ArgoCD** → K8s 内部署：GitOps 声明式，自动同步，应用版本管理
- **OpsMesh** → 非 K8s 部署：裸机/VM 部署、Ansible 编排、脚本执行

**边界规则**：K8s 应用 100% 经 ArgoCD，非 K8s 经 OpsMesh。

## 数据流图

```
                    ┌───────────┐
                    │  Git Repo │
                    │  (声明式)  │
                    └─────┬─────┘
                          │
              ┌───────────┴───────────┐
              ▼                       ▼
     ┌────────────────┐    ┌──────────────────┐
     │    ArgoCD      │    │    OpsMesh       │
     │  (K8s GitOps)  │    │  (非K8s 部署)    │
     │                │    │                  │
     │  自动同步      │    │  Ansible 编排    │
     │  滚动更新      │    │  脚本执行        │
     │  回滚          │    │  配置下发        │
     └───────┬────────┘    └────────┬─────────┘
             │                      │
             ▼                      ▼
     ┌──────────────┐    ┌──────────────────┐
     │  K8s Cluster │    │  裸机 / VM       │
     │  (容器应用)  │    │  (传统应用)      │
     └──────────────┘    └──────────────────┘
```

## 配置示例

### ArgoCD Application（K8s 部署）
```yaml
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: opsmesh-controlplane
spec:
  source:
    repoURL: https://github.com/Levango7/OpsMesh
    path: deploy/helm/opsmesh
    targetRevision: main
  destination:
    server: https://kubernetes.default.svc
    namespace: opsmesh
  syncPolicy:
    automated:
      prune: true
      selfHeal: true
```

### OpsMesh 部署任务（非 K8s）
```json
{
  "task": "deploy",
  "target": "agent-prod-01",
  "command": "ansible-playbook",
  "args": ["/etc/opsmesh/playbooks/deploy-nginx.yml"],
  "timeout": 300
}
```

## 迁移路径

1. **Phase 1**：部署 ArgoCD，将 K8s 应用 Helm Chart 纳入 Git 管理
2. **Phase 2**：ArgoCD 自动同步 K8s 应用
3. **Phase 3**：OpsMesh 保留非 K8s 部署能力
4. **Phase 4**：K8s 部署不再经 OpsMesh 任务引擎
5. **Phase 5**：统一审计日志（ArgoCD + OpsMesh 各自记录）

## 验收标准

- [ ] K8s 应用 100% 经 ArgoCD GitOps
- [ ] 非 K8s 部署经 OpsMesh
- [ ] ArgoCD 自动同步正常
- [ ] 回滚功能可用
- [ ] 部署审计日志完整

## 风险与缓解

| 风险 | 影响 | 缓解措施 |
|------|------|----------|
| Git 仓库不可用 | K8s 部署停滞 | Git 高可用 + 本地缓存 |
| 两套部署路径 | 运维混乱 | 明确边界 + 文档化 |
| ArgoCD 学习成本 | 团队适应期 | 培训 + 模板化 |