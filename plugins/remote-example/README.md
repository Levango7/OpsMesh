# 独立进程插件示例（`plugins/remote-example`）

这是 OpsMesh **插件运行时模型**的参考实现（`docs/tech-debt.md` TD-62 决策点 ①）。
控制面一侧的契约代码在 `internal/plugin/remote.go`，接线在 `internal/controlplane/plugin_remote.go`。

选"独立进程 + HTTP"而不是 Go `plugin.Open` 或 WASM 的原因写在 `internal/plugin/remote.go` 的包注释里，
一句话版：**插件作者不需要和宿主同版本 Go、同依赖图、同平台**，而本产品本来就在交付
"独立进程 + 网络契约"的形态（12 个微服务是 gRPC、告警外发是 HTTP）。

## 快速跑通

```bash
# 1) 起插件（默认只绑 127.0.0.1:9101）
export OPSMESH_PLUGIN_EXAMPLE_TOKEN=$(openssl rand -hex 16)
go run ./plugins/remote-example

# 2) 写清单（令牌只以环境变量名引用，不写进文件）
cat > plugins.json <<'JSON'
{
  "plugins": [
    {
      "name": "capacity-guard",
      "version": "1.0.0",
      "url": "http://127.0.0.1:9101/hook",
      "hooks": ["config.preSet", "config.postSet"],
      "tokenEnv": "OPSMESH_PLUGIN_EXAMPLE_TOKEN",
      "timeoutMs": 2000
    }
  ]
}
JSON

# 3) 起控制面（插件端点在环回，需要显式放开 --plugin-allow-private）
export OPSMESH_PLUGIN_MANIFEST=plugins.json
export OPSMESH_PLUGIN_ALLOW_PRIVATE=true
go run ./cmd/opsmesh --mode=controlplane --store=memory --demo=true
```

之后任何一次 `PUT /api/v1/platform/config` 都会先经过插件：把 `maxTenants` 设成 `0` 会被拒
（HTTP 400，`config update rejected by plugin policy`），插件日志里能看到 `decision=deny` 与原因。

## 契约

### 控制面 → 插件

```
POST <url>
Content-Type: application/json
Authorization: Bearer <token>
X-OpsMesh-Plugin-Hook: <hook>

{"plugin":"capacity-guard","hook":"config.preSet","name":"platform/config","payload":{...}}
```

`payload` 就是该扩展点的宿主负载：

| 扩展点 | 时机 | 负载 | 阻断语义 |
|---|---|---|---|
| `config.preSet` | 平台配置**落库前** | 配置对象（JSON 对象） | 可否决、可改写；失败即拒绝写入 |
| `config.postSet` | 平台配置**落库后** | 配置对象（JSON 对象） | 只记审计，不改变响应（已提交的事实不回滚） |
| `task.preClaim` | agent 领取任务前 | agent ID（JSON 字符串） | 可否决；否决时该次领取不下发任务 |

### 插件 → 控制面

```
HTTP/1.1 200
Content-Type: application/json

{"decision":"deny","reason":"maxTenants=0 会移除租户容量上限策略"}
```

- `decision` 只接受 `allow` / `deny`（缺省 `allow`）；其它值判错，按调用失败处理。
- 空响应体 = 放行。
- `reason` 进宿主日志与审计，**不回吐给 API 客户端**（客户端只看到固定的
  `config update rejected by plugin policy`）；宿主侧对 `reason` 截断到 200 字节。
- 宿主读取响应体上限 1 MiB，超限即判错。

### 可选：改写负载

`config.preSet` 的响应可以带 `payload` 对象，控制面会把它反序列化回**同一份配置对象**后再落库，
所以插件既能否决也能改值：

```json
{"decision":"allow","payload":{"maxTenants":30}}
```

改写只在宿主传入的是指针型负载时生效（`config.preSet`/`config.postSet` 是）。
`task.preClaim` 的负载是字符串，无法就地改写——插件在这种扩展点上回写 `payload`
会得到一次**显式错误**（记为 `outcome="error"`），而不是"看起来生效其实没生效"。

## 必须知道的三条运维性质

1. **fail-closed**：插件进程不可达、超时、返回非 2xx、返回坏 JSON，都等于该扩展点返回错误。
   对 `config.preSet` 这意味着**平台配置写入会被拒绝**。
   反面选择（不可达就放行）会让"把插件停掉"变成一条绕过准入策略的通道，
   所以这里宁可让运维故障可见，也不给静默旁路。
2. **同步在请求路径上**：`config.preSet` 的调用发生在 `PUT` 的处理过程中，
   `timeoutMs` 上限被钉在 30000（默认 2000）。这不是保守，是"一个卡住的插件不能拖死控制面 API"。
3. **令牌是双向的**：控制面用 `Authorization: Bearer` 证明"是我在调用"，
   示例插件对不匹配的令牌回 401 而不是 200——401 会被宿主记成 `outcome="error"`，
   配错令牌会立刻在指标上显形，而不是变成"插件从没反对过"。

## 可观测面

| 序列 | 含义 |
|---|---|
| `opsmesh_plugin_remote_plugins` | 本次进程注册的独立进程插件数（0 = 能力已交付但未启用） |
| `opsmesh_plugin_hook_calls_total{hook,outcome}` | 调用次数；`outcome` ∈ `ok` / `denied` / `error` |

`denied` 与 `error` 的区分是这条链路最重要的可观测点：
前者说明策略正在起作用，后者是要打电话的故障。出厂告警见
`deploy/monitoring/prometheus-alerts.yml` 的 `OpsMeshPluginHookFailed`。

**插件名刻意不进指标标签**：插件名来自运维自写的清单，是自由文本，
入标签等于把基数控制权交给配置文件（同 `opsmesh_agent_signature_verifications_total` 的判据）。
定位到具体插件靠日志与审计里的名字。

## 写自己的插件

任何语言都可以，只要满足上面的契约。最小实现三件事：

1. 校验 `Authorization: Bearer <约定的令牌>`；
2. 解析 `{"plugin","hook","name","payload"}`；
3. 返回 200 与 `{"decision":"allow"}`，或 `{"decision":"deny","reason":"..."}`。

清单里未定义的字段会被**判错而不是忽略**（`DisallowUnknownFields`），
所以 `timeoutMs` 拼成 `timeout_ms`、`tokenEnv` 拼成 `token` 都会在启动时立刻报错。
生产模式（`--production=true`）下清单解析/校验失败会终止启动。
