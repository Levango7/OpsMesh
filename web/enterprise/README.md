# OpsMesh 企业版前端（web/enterprise）

Vue 3 + Vite + Pinia 的企业版 SPA，由控制面以 `/enterprise/` 前缀分发（构建产物 `dist/`）。

## 常用命令

```bash
npm run dev      # 开发服务器（:5174，/api 代理到 :8080，可用 VITE_API_PROXY_TARGET 覆盖）
npm run build    # 生产构建（terser 压缩 + gzip/brotli 预压缩产物）
npm run lint     # ESLint（eslint src --fix）
npm run test     # Vitest 全量单测
npm run e2e      # Playwright E2E
```

## 双平台构建约束（重要）

本仓库的 `node_modules` 可能被 **Windows 与 WSL（Linux）共同使用**（同一份目录、同一挂载点）。
npm 在安装时按**当前平台**过滤 optional 原生依赖，因此以下原生二进制包决定"哪一侧能构建"：

| 包 | Windows 需要 | Linux(WSL) 需要 |
|----|-------------|-----------------|
| rollup 原生 | `@rollup/rollup-win32-x64-msvc` | `@rollup/rollup-linux-x64-gnu`（musl 系统用 `-musl`） |
| esbuild 原生 | `@esbuild/win32-x64` | `@esbuild/linux-x64` |

**行为**：在任一侧执行 `npm install` 会补齐当前平台的原生包，并**裁剪**（prune）另一平台的包，
导致另一侧 `vite build / vitest` 报 `Cannot find module '@rollup/rollup-<platform>'`。

**恢复方法**（三选一）：

1. 在该平台重新 `npm install`（适合该平台是主开发平台）；
2. 手动补包（不触发 prune，两平台共存）：

   ```bash
   npm pack @rollup/rollup-win32-x64-msvc@<rollup版本>   # 或 linux-x64-gnu
   npm pack @esbuild/win32-x64@<esbuild版本>             # 或 linux-x64
   tar -xzf rollup-rollup-win32-x64-msvc-*.tgz
   # 将 package/ 解压结果移动到 node_modules/@rollup/rollup-win32-x64-msvc/
   ```

   （rollup/esbuild 的版本以 `node_modules/rollup/package.json`、`node_modules/esbuild/package.json` 为准）
3. 统一构建入口：仅在 WSL 侧执行前端构建与部署资产验证
   （`deploy/scripts/validate-deploy-assets.sh` 本就以 WSL 为基准环境）。

本仓库当前已手工补齐 win32 与 linux 两套原生包，Windows 与 WSL 均可直接 `npm run build` / `npm run test`；
在任一侧重新 `npm install` 后，需按上述方法恢复对侧。

## 目录结构

```
src/
  api/         # REST 接入层（request.js 统一封装：双 HttpOnly Cookie 鉴权 + 401 静默刷新）
  views/       # 页面视图（路由在 src/router/index.js，导航分组在 src/App.vue navGroups）
  stores/      # Pinia stores
  components/  # 通用组件（Icon/DataTable/DetailDrawer/ConfirmModal/MetricsCard 等）
  i18n/        # 自实现 i18n：common 域首屏加载，其余按路由域懒加载（locales/{zh,en}/*.json）
  assets/tokens.css  # 设计令牌（颜色/间距/卡片）
```

## 基础设施运维台（/infra-console）

P4/P5/P6 全部运维能力的统一入口（17 个 domain：中间件/数据库/OLAP/时序/向量/图/LB/存储/
Harbor/监控兼容/Kernel/安全/虚拟化/Ansible/包管理/环境依赖/K8s 一键部署）。

- **数据驱动**：domain 与动作目录集中在 `src/api/infra-ops.js`（`INFRA_DOMAINS`），
  视图 `src/views/InfraConsoleView.vue` 按目录动态渲染表单；新增能力只需追加目录条目。
- **字段类型**：`device`（设备下拉）/`text`/`number`/`select`/`bool`/`textarea`。
- **执行模型**：查询类动作（GET）直出 JSON；执行类动作（POST/DELETE）返回 Task 卡片，
  经 `GET /tasks/{id}/result` 拉取 agent 输出。
- **测试**：`src/views/__tests__/InfraConsoleView.spec.js` 含目录结构完整性校验
  （每个 domain/action/field 的结构与双语 label）。
