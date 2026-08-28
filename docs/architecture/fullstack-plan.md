# OpsMesh 全栈推进计划

## 现状评估

### 前端（Vue3 已有）
- **已有视图**：17 个（Devices, Alerts, Tasks, Deploys, CMDB, K8s, Logs, Workflows, Users, Roles, Permissions 等）
- **技术栈**：Vue 3 + Vite + Pinia + Vue Router + Vitest + Playwright
- **API 层**：12 个 API 模块（auth, device, alert, task, deploy, cmdb, k8s, log, middleware, os-optimize, secrets, workflow）
- **缺失**：新微服务（gpu, bot, runbook, workflow, incident, autoscaler, plugin, portal）无对应视图

### 后端（Go 微服务）
- **API 契约**：17 个微服务，350+ 测试
- **存储**：全部 Memory Store（无持久化）
- **集成**：GPU/K8s/Ollama/Prometheus 全部模拟

## 推进计划

### Phase A：存储层替换（生产基础）
| 服务 | 存储 | 工作量 |
|------|------|--------|
| auth-svc | MySQL (users/roles/permissions) | 3 天 |
| device-svc | MySQL (devices/agents/ci_items) | 3 天 |
| task-svc | MySQL (tasks/results) | 2 天 |
| alert-svc | MySQL (alerts/rules) | 2 天 |
| config-svc | MySQL (configs/secrets) | 2 天 |
| portal-svc | MySQL (requests/quotas) | 2 天 |

### Phase B：前端扩展（用户可见）
| 视图 | 功能 | 工作量 |
|------|------|--------|
| GPU 管理 | 节点/工作负载/模型/指标 | 3 天 |
| ChatOps | Bot 命令面板 | 2 天 |
| Runbook | Playbook 编辑器+执行 | 3 天 |
| 工作流 | DAG 设计器+审批 | 5 天 |
| 事件管理 | 时间线+复盘 | 3 天 |
| 自动扩缩 | 规则配置+决策历史 | 2 天 |
| 插件市场 | 搜索/安装/版本 | 2 天 |
| 自助门户 | 资源申请/审批/成本 | 3 天 |

### Phase C：真实集成（生产可用）
| 集成 | 方案 | 工作量 |
|------|------|--------|
| GPU 检测 | nvidia-smi 解析 | 3 天 |
| K8s API | client-go | 5 天 |
| Ollama | HTTP API 调用 | 2 天 |
| Prometheus | prometheus/client_golang | 3 天 |

### Phase D：服务间认证
| 组件 | 方案 | 工作量 |
|------|------|--------|
| mTLS | cert-manager + Istio | 3 天 |
| JWT 传播 | 服务间 Token 传递 | 2 天 |

## 执行顺序
```
Phase A (存储) → Phase B (前端) → Phase C (集成) → Phase D (认证)
```

## 总工作量估算
- Phase A: 14 天
- Phase B: 23 天
- Phase C: 13 天
- Phase D: 5 天
- **总计: ~55 人天**
