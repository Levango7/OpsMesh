# 09 - CI 引擎迁移：Tekton vs Jenkins

## 冲突描述

Tekton 和 Jenkins 都想做 CI 引擎：
- **Tekton**：K8s 原生、声明式、Pipeline as Code
- **Jenkins**：成熟生态、丰富插件、GUI 配置

新老项目选择困难：新项目用 Tekton 但老项目迁移成本高。

## 解决方案

**渐进迁移**：
- **新项目** → Tekton：K8s 原生、声明式、与 ArgoCD 集成
- **老项目** → 保留 Jenkins：渐进迁移，不强制切换
- **迁移工具** → Jenkinsfile 转 Tekton Pipeline：自动化转换

**边界规则**：新项目 100% Tekton，老项目提供迁移工具和过渡期。

## 数据流图

```
                    ┌───────────┐
                    │  Git Repo │
                    └─────┬─────┘
                          │
              ┌───────────┴───────────┐
              ▼                       ▼
     ┌────────────────┐    ┌──────────────────┐
     │    Tekton      │    │    Jenkins       │
     │  (新项目 CI)   │    │  (老项目 CI)     │
     │                │    │                  │
     │  Pipeline YAML │    │  Jenkinsfile     │
     │  K8s 原生      │    │  插件生态        │
     └───────┬────────┘    └────────┬─────────┘
             │                      │
             ▼                      ▼
     ┌──────────────┐    ┌──────────────────┐
     │   ArgoCD     │    │  迁移工具         │
     │  (CD 部署)   │    │  Jenkinsfile →   │
     │              │    │  Tekton Pipeline │
     └──────────────┘    └────────┬─────────┘
                                  │
                                  ▼
                         ┌──────────────────┐
                         │    Tekton        │
                         │  (迁移后 CI)     │
                         └──────────────────┘
```

## 配置示例

### Tekton Pipeline（新项目）
```yaml
apiVersion: tekton.dev/v1beta1
kind: Pipeline
metadata:
  name: build-deploy
spec:
  tasks:
    - name: build
      taskRef:
        name: kaniko-build
      params:
        - name: image
          value: "$(params.image)"
    - name: deploy
      runAfter: [build]
      taskRef:
        name: argocd-sync
      params:
        - name: app
          value: "$(params.app)"
```

### 迁移工具（Jenkinsfile → Tekton）
```yaml
# j2t（Jenkins to Tekton）转换工具
migrate:
  source: "Jenkinsfile"
  target: "tekton-pipeline.yaml"
  mapping:
    - jenkins_stage: "Build"
      tekton_task: "kaniko-build"
    - jenkins_stage: "Test"
      tekton_task: "go-test"
    - jenkins_stage: "Deploy"
      tekton_task: "argocd-sync"
  plugins:
    - "pipeline-utility-steps" → "tekton-catalog-utils"
    - "docker" → "kaniko"
```

## 迁移路径

1. **Phase 1**：部署 Tekton + Tekton Triggers
2. **Phase 2**：新项目使用 Tekton Pipeline
3. **Phase 3**：开发 Jenkinsfile → Tekton 转换工具
4. **Phase 4**：老项目分批迁移（按团队/按复杂度）
5. **Phase 5**：Jenkins 保留过渡期后下线

## 验收标准

- [ ] 新项目 100% Tekton
- [ ] 老项目保留 Jenkins
- [ ] 迁移工具可用（Jenkinsfile → Tekton）
- [ ] Tekton + ArgoCD 集成正常
- [ ] CI/CD 全链路可观测

## 风险与缓解

| 风险 | 影响 | 缓解措施 |
|------|------|----------|
| 迁移工具不完美 | 复杂 Pipeline 转换失败 | 人工审查 + 渐进迁移 |
| Jenkins 插件无替代 | 功能缺失 | Tekton Catalog 找替代 |
| 双引擎运维 | 资源浪费 | 迁移完成后下线 Jenkins |