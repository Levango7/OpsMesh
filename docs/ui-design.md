# OpsMesh UI 设计文档

> 适用范围：OpsMesh 控制台前端（个人版引导页 + 企业版 Vue 3 应用）
> 设计基调：收敛低调、靛蓝主色、信息密度优先、零第三方 UI 框架依赖
> 维护者：前端组 · 版本 v0.4.0+ · 最后更新 2026-08-17

---

## 第1章 设计总览

### 1.1 设计目标

OpsMesh 控制台面向运维工程师与平台管理员，需在单屏内呈现大量设备、任务、告警、配置项等结构化信息。UI 设计遵循以下原则：

- **信息密度优先**：默认字号 13.5px，表格行高紧凑，单屏可承载更多数据。
- **收敛低调**：低饱和度靛蓝主色，避免高对比强光配色；动画时长统一在 0.15s–0.35s，拒绝浮夸过渡。
- **零依赖**：组件库自研，图标、图表、关系图谱均纯 SVG 实现，不引入 Element Plus / Ant Design / ECharts 等重型框架。
- **双前端共存**：个人版（原生 JS 引导页）与企业版（Vue 3 + Vite + Pinia）共享设计 token，但仅企业版承载完整业务功能。
- **主题与语言可切换**：light/dark 双主题 + zh/en 双语，运行时切换并持久化。

### 1.2 双前端策略

OpsMesh 自 v0.4.0 起将完整 UI 套件收敛至企业版前端，个人版仅保留引导页。两者关系如下：

表：双前端对照表

| 维度 | 个人版 | 企业版 |
|---|---|---|
| 路径 | `internal/controlplane/web/` | `web/enterprise/` |
| 访问入口 | `/` | `/enterprise/` |
| 技术栈 | 原生 ES Module + 单页 HTML | Vue 3.5 + Vite 5 + Pinia 2 + Vue Router 4 |
| 样式范围 | `assets/app.css`（19 行最小样式） | `src/assets/tokens.css`（182 行设计 token） + 各组件 scoped 样式 |
| 功能职责 | 引导跳转至 `/enterprise/`，3 秒自动重定向 | 全部业务功能：设备/任务/告警/CMDB/作业编排/部署/日志/用户/角色/权限/密钥 |
| 主题机制 | `assets/theme.js` 仅保留 stub（满足契约测试） | `src/stores/theme.js` Pinia store + `data-theme` 属性切换 |
| 设计 token | 内联 CSS 变量（`--bg`/`--accent` 等 7 个） | 完整 token 体系（背景/表面/主色/四色搭配/状态色/圆角/阴影/间距/字号/字体） |
| 共享约定 | 与企业版共用靛蓝主色 `#4f46e5` 系、`system-ui` 字体栈、12px 圆角 | 同左 |

个人版 `index.html` 内嵌样式仅用于引导卡片，色值与企业版 token 保持同源（如 `--accent:#4f46e5` 对应企业版 `--indigo`），确保跳转前后视觉连续。

### 1.3 文件结构

```text
opsmesh/
├── internal/controlplane/web/          # 个人版（引导页）
│   ├── index.html                      # 引导卡片 + 3s 跳转
│   └── assets/
│       ├── app.css                     # 最小样式（19 行）
│       ├── main.js                     # ES module 跳转脚本
│       └── theme.js                    # 主题 stub
└── web/enterprise/                     # 企业版（Vue 3）
    ├── index.html                      # SPA 入口
    └── src/
        ├── App.vue                     # 顶栏 + 侧栏 + 内容 + 底栏布局
        ├── main.js                     # 应用入口（Pinia + Router + i18n + 主题初始化）
        ├── assets/tokens.css           # 设计 token（light + dark）
        ├── components/                 # 自研组件库（10 个 .vue + 对应 .test.js）
        ├── composables/                # 组合式工具（useFormatTime）
        ├── i18n/                       # zh.json / en.json / index.js
        ├── router/                     # 路由表 + 守卫
        ├── stores/                     # Pinia stores（13 个领域 store）
        └── views/                      # 页面视图（17 个 View.vue）
```

---

## 第2章 设计系统

### 2.1 色彩体系

#### 2.1.1 主色与功能色

主色采用靛蓝（Indigo），用于操作按钮、链接、激活态、品牌标识。搭配四色用于不同业务模块的视觉区分，避免单一色调疲劳。

表：主色与功能色定义（light 主题）

| Token | 色值 | 用途 |
|---|---|---|
| `--accent` | `#5b5ef0` | 主操作色（primary 按钮、激活态） |
| `--accent-2` | `#6366f1` | 主色悬停态 |
| `--accent-soft` | `#e7eaf3` | 主色浅底（激活行背景、徽章底） |
| `--indigo` | `#6366f1` | 靛蓝（作业编排模块） |
| `--teal` | `#0d9488` | 青色（中间件部署、成功强调） |
| `--amber` | `#d97706` | 琥珀（告警 warning、OS 优化） |
| `--rose` | `#e11d48` | 玫红（失败、critical 告警） |
| `--sky` | `#0284c7` | 天蓝（K8s 管理、信息提示） |
| `--green` | `#059669` | 绿色（纳管成功、健康状态） |

每个功能色均配对 `*-soft` 浅底版本，用于徽章背景、卡片图标底色等低饱和场景，保证文字与背景对比度 ≥ 4.5:1。

#### 2.1.2 背景与表面

采用三层表面体系，通过明度梯度建立层次感：

表：背景与表面 token（light / dark）

| Token | light | dark | 用途 |
|---|---|---|---|
| `--bg` | `#e8ecf7` | `#0f1424` | 页面底色 |
| `--bg-soft` | `#eef1f9` | `#161c30` | 悬停底色、表头底 |
| `--surface` | `#f7f8fd` | `#1a2138` | 卡片、侧栏、抽屉表面 |
| `--surface-2` | `#ffffff` | `#232b45` | 输入框、弹窗内表面对比层 |
| `--surface-3` | `#eef1f9` | `#1e2640` | 表头、徽章底、tab 编号底 |
| `--border` | `#e1e6f2` | `#2a3454` | 主边框 |
| `--border-2` | `#cfd6ea` | `#364266` | 输入框边框（稍深以聚焦） |

dark 主题采用深蓝灰（非纯黑），保留靛蓝主色但降低亮度，状态色提高亮度保证可读性。

#### 2.1.3 文字色

表：文字色 token

| Token | light | dark | 用途 |
|---|---|---|---|
| `--text` | `#1f2540` | `#e6ebf5` | 主文字（标题、正文） |
| `--text-2` | `#525a78` | `#a8b1cc` | 次要文字（描述、表头） |
| `--text-3` | `#6b7390` | `#7a8499` | 辅助文字（hint、底栏） |

#### 2.1.4 状态色

状态色与功能色复用，但提供语义化别名与配套底色，用于 StatusBadge、ProgressRing、消息提示：

表：状态色 token

| Token | light | dark | 语义 |
|---|---|---|---|
| `--ok` / `--ok-bg` | `#059669` / `#d6f3e8` | `#34d399` / `#064e3b` | 成功 / 已纳管 / done |
| `--fail` / `--fail-bg` | `#e11d48` / `#ffe4ec` | `#fb7185` / `#4c1d2f` | 失败 / error / critical |
| `--warn` / `--warn-bg` | `#d97706` / `#fef3e2` | `#fbbf24` / `#78350f` | 警告 / running / warning |
| `--info` / `--info-bg` | `#6366f1` / `#e7eaf3` | `#818cf8` / `#2a2f55` | 信息 / pending / created |

### 2.2 字体

#### 2.2.1 字体栈

```css
--font: system-ui, -apple-system, "Segoe UI", Roboto, "Helvetica Neue", Arial, "PingFang SC", "Microsoft YaHei", sans-serif;
--font-mono: "SFMono-Regular", Consolas, "Liberation Mono", Menlo, monospace;
```

优先使用系统原生字体，避免 webfont 加载开销；中文环境自动 fallback 到 PingFang SC（macOS）与 Microsoft YaHei（Windows）。等宽字体用于代码、ID、数值。

#### 2.2.2 字号阶梯

表：字号 token

| Token | 像素值 | 用途 |
|---|---|---|
| `--fs-xs` | 11.5px | tab 编号、徽章、图例 |
| `--fs-sm` | 12.5px | 次要文字、表头、hint |
| `--fs-base` | 13.5px | 正文、按钮、输入框（默认） |
| `--fs-md` | 14px | 卡片标题 h3、强调正文 |
| `--fs-lg` | 16px | 页面标题 h2、品牌名 |
| `--fs-xl` | 18px | 文档标题 h1 |

行高统一 1.6，标题 `letter-spacing: -.01em` 微收紧。数值类文字（计数、百分比）使用 `font-variant-numeric: tabular-nums` 保证等宽对齐。

### 2.3 间距

采用 4px 基准网格，6 阶间距 token：

表：间距 token

| Token | 值 | 典型用途 |
|---|---|---|
| `--space-1` | 4px | 图标与文字间隙、徽章内边距 |
| `--space-2` | 8px | 按钮组 gap、卡片内小间距 |
| `--space-3` | 12px | 卡片标题与正文、侧栏 tab 内边距 |
| `--space-4` | 16px | 卡片 padding、行间主间距 |
| `--space-5` | 20px | 内容区 padding、row gap |
| `--space-6` | 24px | 大区块分隔、抽屉 padding |

### 2.4 圆角

表：圆角 token

| Token | 值 | 用途 |
|---|---|---|
| `--radius` | 14px | 卡片、弹窗、抽屉外层 |
| `--radius-sm` | 9px | 按钮、输入框、表格、tab |
| `999px` | — | 徽章、chip（胶囊形） |

### 2.5 阴影

采用双层阴影（近距 + 远距）模拟柔和浮起感，dark 主题下减弱：

```css
/* light */
--shadow: 0 1px 2px rgba(31,37,64,.05), 0 6px 20px rgba(31,37,64,.07);
/* dark */
--shadow: 0 1px 2px rgba(0,0,0,.3), 0 6px 20px rgba(0,0,0,.4);
```

特殊场景阴影：

- 弹窗：`0 12px 40px rgba(31,37,64,0.22)`（更强浮起）
- 抽屉：`--drawer-shadow: -8px 0 30px rgba(31,37,64,.14)`（左侧方向阴影）
- 品牌图标：`0 4px 14px rgba(99,102,241,.4)`（主色光晕）
- primary 按钮：`0 2px 8px rgba(91,94,240,.32)`（主色光晕）

### 2.6 动画

所有动画时长统一在 0.15s–0.35s 区间，缓动函数以 `ease` 为主，避免弹性/回弹等浮夸过渡：

表：动画时长规范

| 场景 | 时长 | 触发 |
|---|---|---|
| 按钮/输入框 hover、focus | 0.15s | 颜色、边框过渡 |
| 表格行 hover | 0.12s | 背景色过渡 |
| 路由切换淡入淡出 | 0.2s | opacity + translateY 4px |
| 弹窗进出 | 0.18s | opacity + translateY(-8px) scale(0.98) |
| 抽屉进出 | 0.22s | translateX(100%) |
| ProgressRing 进度变化 | 0.35s | stroke-dashoffset |

路由切换使用 Vue `<transition name="fade" mode="out-in">`，确保旧组件先离开再进入新组件，避免重叠。

---

## 第3章 组件库

企业版组件库位于 `web/enterprise/src/components/`，全部自研，每个组件配套 `.test.js` 单元测试（Vitest + @vue/test-utils）。共 10 个组件：

表：组件库清单

| 组件 | 文件 | 职责 | 关键 props |
|---|---|---|---|
| Icon | `Icon.vue` | 单色填充 SVG 图标 | `name`, `size` |
| DataTable | `DataTable.vue` | 数据表格（排序/空态/自定义 slot） | `columns`, `rows`, `sortKey`, `clickable` |
| ConfirmModal | `ConfirmModal.vue` | 确认/信息对话框 | `modelValue`, `title`, `message`, `info` |
| PromptModal | `PromptModal.vue` | 输入对话框 | `modelValue`, `title`, `defaultValue` |
| DetailDrawer | `DetailDrawer.vue` | 右侧滑出抽屉 | `open`, `title` |
| MetricsCard | `MetricsCard.vue` | 指标卡片（标题+图标+插槽） | `title`, `icon`, `accent` |
| Pagination | `Pagination.vue` | 简易分页（上/下页） | `page`, `pageSize`, `limit` |
| ProgressRing | `ProgressRing.vue` | 圆环进度条（SVG） | `value`, `size`, `warnAt`, `dangerAt` |
| StatusBadge | `StatusBadge.vue` | 状态徽章 | `status`, `text` |
| RelationGraph | `RelationGraph.vue` | CMDB 关系图谱（力导向/拓扑） | `graph`, `mode`, `width`, `height` |

### 3.1 按钮

按钮通过全局 `tokens.css` 中的 `button` 选择器定义，无独立组件，使用 class 组合：

表：按钮 variant

| class | 外观 | 用途 |
|---|---|---|
| 默认 | 灰底 `--surface-3` | 次要操作、取消 |
| `.primary` | 靛蓝底白字 + 光晕 | 主操作（确认、提交） |
| `.teal` | 青底白字 | 中间件部署等独立流程 |
| `.danger` | 玫红底白字 | 删除、强制下线 |
| `.outline` | 透明底 + 边框 | 弹窗取消、抽屉关闭 |
| `.xs` | 高 26px、字号 sm | 表格行内操作、工具栏 |

按钮交互态：hover 切换底色、active `translateY(1px)` 模拟按下、disabled `opacity:.55` + `not-allowed`。

代码示例：按钮基础用法（HTML）

```html
<button class="primary">提交</button>
<button class="outline">取消</button>
<button class="danger xs" :disabled="!selected">删除</button>
```

### 3.2 卡片（MetricsCard）

`MetricsCard` 为通用指标卡片，统一标题 + 可选图标 + 内容插槽 + 操作插槽：

代码示例：MetricsCard 用法（Vue）

```vue
<MetricsCard title="设备总数" icon="device" accent="--indigo">
  <div class="big-num">{{ total }}</div>
  <template #actions>
    <button class="xs outline" @click="refresh">刷新</button>
  </template>
</MetricsCard>
```

卡片样式：`--surface` 底 + `--border` 边框 + `--radius` 圆角 + `--shadow` 阴影，padding 16px 18px。图标置于 28×28 圆角方框内，可指定 `accent` CSS 变量名改变图标颜色。

通用 `.card` 工具类提供相同外观，用于不需要标题图标结构的场景。

### 3.3 表格（DataTable）

`DataTable` 支持自定义单元格 slot、格式化函数、行点击、受控/非受控排序：

代码示例：DataTable 用法（Vue）

```vue
<DataTable
  :columns="[
    { key: 'name', title: '名称', sortable: true },
    { key: 'status', title: '状态', slot: 'status' },
    { key: 'created', title: '创建时间', format: fmtTime }
  ]"
  :rows="devices"
  row-key="id"
  clickable
  @row-click="onSelect"
>
  <template #status="{ value }">
    <StatusBadge :status="value" />
  </template>
</DataTable>
```

特性：

- **排序**：点击表头 asc → desc → 取消三态循环；支持自定义 `col.sort(a,b)` 比较函数；null/undefined 统一排末尾；数字按数值比较，字符串按 `localeCompare`（支持中文）。
- **空状态**：`rows` 为空时渲染整行 `colspan` 占位，文案走 i18n `common.no_data` 或 `emptyText` prop。
- **行点击**：`clickable` 开启后行 hover 变 `--accent-soft` 底，点击 emit `row-click`。
- **受控排序**：传入 `sortKey` + `sortOrder` 时由外部控制，组件 emit `sort-change` 通知；否则使用内部状态。

样式：`border-collapse: separate` + `--radius-sm` 外圆角，表头 `--surface-3` 底 + 12px 字号 + `letter-spacing:.02em`，行 hover `--bg-soft`。

### 3.4 表单控件

输入框、选择框、文本域通过全局 `input, select, textarea` 选择器统一样式：

- 背景 `--surface-2`、边框 `--border-2`、padding 7px 10px、圆角 `--radius-sm`。
- focus 态：边框变 `--accent` + `0 0 0 3px var(--accent-soft)` 光环。
- label：`inline-flex` + gap 6px、字号 13px、色 `--text-2`。

复杂表单（登录、注册、用户编辑）在各自 View 内组合，未抽取独立 Form 组件，保持轻量。

### 3.5 对话框（ConfirmModal / PromptModal）

#### 3.5.1 ConfirmModal

替代原生 `confirm()` / `alert()`，通过 `<Teleport to="body">` 渲染到 body 末尾，避免被父级 `overflow` 裁切。

代码示例：ConfirmModal 用法（Vue）

```vue
<ConfirmModal
  v-model="showDelete"
  title="删除设备"
  message="确认删除 device-001？此操作不可撤销。"
  @confirm="doDelete"
/>
```

模式：

- **普通模式**：显示「取消 + 确认」两按钮，点击遮罩或 ESC 触发 cancel。
- **info 模式**（`info` prop）：仅显示「确定」按钮，点击遮罩不关闭（强制用户确认）。

#### 3.5.2 PromptModal

替代原生 `prompt()`，提供单行输入对话框。打开时自动聚焦输入框并全选默认值，回车确认，ESC 取消。

代码示例：PromptModal 用法（Vue）

```vue
<PromptModal
  v-model="showRename"
  title="重命名"
  :default-value="currentName"
  @confirm="onRename"
/>
```

两者共享遮罩与弹窗样式：遮罩 `rgba(31,37,64,0.42)` + flex 居中，弹窗 `--surface` 底 + `--radius` 圆角 + `0 12px 40px` 阴影，最大宽 420px。进出动画 0.18s opacity + translateY(-8px) scale(0.98)。

### 3.6 抽屉（DetailDrawer）

右侧滑出抽屉，用于详情面板（设备详情、CI 详情）。宽度 `min(460px, 92vw)`，高度 100vh，左侧 `--drawer-shadow` 阴影。

代码示例：DetailDrawer 用法（Vue）

```vue
<DetailDrawer :open="open" :title="device.name" @close="open = false">
  <h4>基本信息</h4>
  <table>...</table>
  <h4>指标</h4>
  <ProgressRing :value="cpuUsage" />
</DetailDrawer>
```

进出动画 0.22s `translateX(100%)`。内部 `:deep()` 样式作用于插槽内 table，保持抽屉内表格与 DataTable 视觉一致。

### 3.7 导航

导航在 `App.vue` 内实现，分顶栏与侧栏两部分（详见第4章）。侧栏采用分组 + 全局连续编号设计：

- 分组标题：11px、`uppercase`、`letter-spacing:.06em`、色 `--text-3`。
- tab 项：编号方框（20×20、`--radius-sm`）+ 图标 + 文字；激活态 `--accent-soft` 底 + `--accent` 文字 + 编号方框变主色实心。
- 编号即「功能入口序号」，用户点击即可执行对应操作，符合运维场景的「编号直达」习惯。

### 3.8 图标（Icon）

`Icon.vue` 为单色填充 SVG 图标组件，24×24 viewBox，`fill="currentColor"` 继承文字颜色便于主题切换。图标字典内置 30+ 图标，按功能模块、顶栏、业务实体、状态、操作、品牌分类。

设计原则：

- **风格统一**：全部 Boxicons Solid 风格（圆润、实心、辨识度高），拒绝线性/双色混搭。
- **独一无二**：每个功能模块图标不重复（home/ops/cmdb/deploy/flow/logs/alerts/users/roles/permissions 各异）。
- **简约小众**：避免花哨图标，优先几何感强的实心填充。
- **无障碍**：`aria-hidden="true"`，装饰性图标不朗读。

代码示例：Icon 用法（Vue）

```vue
<Icon name="device" :size="16" />
<Icon name="alerts" :size="18" />
```

未匹配 name 时回退到 `home` 图标，避免渲染空白。

### 3.9 图表

#### 3.9.1 ProgressRing

圆环进度条，纯 SVG 实现，通过 `stroke-dasharray` + `stroke-dashoffset` 控制进度，中心展示百分比文字。颜色按阈值自动切换：

- `value < warnAt`（默认 60）：`--ok` 绿色
- `warnAt ≤ value < dangerAt`（默认 85）：`--warn` 琥珀
- `value ≥ dangerAt`：`--fail` 玫红

可通过 `color` prop 覆盖阈值判定。进度变化 0.35s 过渡动画。

#### 3.9.2 RelationGraph

CMDB 关系图谱可视化组件，纯 SVG + 轻量力模拟，零第三方依赖：

- **力导向布局**（force）：斥力 + 弹簧力 + 中心引力，迭代收敛。
- **网络拓扑布局**（topology）：按 CI 类型分层，体现 cluster → machine → os → service → app 层次。
- **交互**：节点拖拽、点击选中、hover 高亮、滚轮缩放、空白拖拽平移。
- **视觉编码**：不同 CI 类型用不同颜色，不同关系类型用不同线型（实线/虚线）+ 箭头。
- **辅助**：工具栏（布局切换/缩放/重置）、图例（类型 + 关系）、节点详情浮层、空状态。

布局算法为确定性实现，便于单元测试断言节点位置。

### 3.10 状态徽章（StatusBadge）

`StatusBadge` 将后端多种状态字符串归一化到四类视觉：

表：StatusBadge 状态映射

| 输入 status | 输出 class | 视觉 |
|---|---|---|
| ok / success / done / managed / acknowledged | `ok` | 绿底绿字 |
| fail / failed / error / critical | `fail` | 玫红底玫红字 |
| warn / warning / running / rolledback | `warn` | 琥珀底琥珀字 |
| info / pending / created / draft / discovered / silenced | `info` | 靛蓝底靛蓝字 |

样式：胶囊形（`border-radius:999px`）、高 20px、字号 11.5px、font-weight 600。

### 3.11 分页（Pagination）

简易上/下页分页，显示当前页与已加载数量信息，按钮在无更多数据时 disabled。适用于按 limit 分批加载的列表（设备、告警、日志）。

代码示例：Pagination 用法（Vue）

```vue
<Pagination :page="page" :page-size="list.length" :limit="200"
  @prev="page--" @next="loadMore" />
```

---

## 第4章 页面布局

### 4.1 整体结构

企业版已登录态采用「顶栏 + 侧栏 + 内容 + 底栏」四区布局，未登录态仅渲染路由（登录/注册页全屏自管布局）。

```text
┌─────────────────────────────────────────────┐
│  appbar（sticky, 玻璃质感）                  │  ← 顶栏
├──────────┬──────────────────────────────────┤
│          │                                  │
│ sidebar  │  content (max-width 1200px)      │  ← 主区
│ (210px)  │                                  │
│          │                                  │
├──────────┴──────────────────────────────────┤
│  footbar                                     │  ← 底栏
└─────────────────────────────────────────────┘
```

布局容器 `.app` 使用 `flex-direction:column; min-height:100vh`，主区 `.layout` 使用 `flex:1` 撑满剩余高度。

### 4.2 顶栏（appbar）

顶栏 `position:sticky; top:0; z-index:30`，背景 `--appbar-bg`（带 0.86 透明度的 `--surface`）+ `backdrop-filter:blur(10px)` 玻璃质感，底部 1px `--border` 分隔线。

左右分区：

- **左侧 brand**：38×38 圆角 logo（靛蓝→天蓝渐变 + 主色光晕）+ 标题 h1（16px）+ 副标题（12px、`--text-3`）。
- **右侧 appbar-right**：
  - 资源统计 chip 组：设备/纳管/告警三个胶囊 chip，数值加粗 + tabular-nums。
  - 主题切换按钮（icon-btn，根据当前主题显示对应图标）。
  - 语言切换按钮（icon-btn + 中/EN 文字标签）。
  - 用户信息胶囊（`--accent-soft` 底 + 头像 + 用户名）。
  - 退出按钮（icon-btn danger，hover 变玫红）。

icon-btn 统一样式：高 34px、`--surface-3` 底、`--border` 边框、`--radius-sm` 圆角。

### 4.3 侧边栏（sidebar）

侧栏 `flex:0 0 210px`，背景 `--surface`，右侧 1px `--border` 分隔线，padding 14px 10px，`overflow:auto`。

#### 4.3.1 分组导航

导航按业务域分 6 组，每组带 `labelKey` 走 i18n：

表：侧栏分组与功能入口

| 分组 | labelKey | 入口 |
|---|---|---|
| 概览 | `nav.overview` | 总览 |
| 运维管理 | `nav.ops` | 设备纳管 / 任务下发 / 监控告警 / OS 优化 / 中间件部署 / K8s 管理 |
| 资产配置 | `nav.assets` | 配置项 CMDB |
| 交付中心 | `nav.delivery` | 作业编排 / 部署中心 |
| 可观测性 | `nav.observability` | 日志检索 |
| 系统管理 | `nav.system` | 用户中心 / 角色管理 / 权限管理 / 密钥管理 |

每个入口按当前用户权限过滤（`authStore.hasPerm(it.required)`），过滤后赋予全局连续编号 1、2、3…。编号方框在激活态变主色实心白字，强化「当前所在位置」。

#### 4.3.2 API 信息区

侧栏底部虚线分隔后展示 API base / Web base 信息，12px、`--text-3`、行高 1.8，便于运维快速定位后端地址。

### 4.4 内容区（content）

内容区 `flex:1; max-width:1200px; margin:0 auto; padding:22px`，居中约束最大宽度避免超宽屏拉伸。路由切换通过 `<transition name="fade" mode="out-in">` 包裹 `<router-view>`，0.2s 淡入淡出 + 4px 垂直位移。

### 4.5 底栏（footbar）

底栏 `flex` 两端对齐，背景 `--surface`，顶部 1px `--border`，padding 10px 24px，字号 12px、色 `--text-3`。左侧显示 `app.footer`（技术栈信息），右侧显示 `app.copyright`（版权）。

### 4.6 响应式断点

表：响应式断点

| 断点 | 行为 |
|---|---|
| `max-width: 768px` | 侧栏变全宽固定定位 + `translateX(-100%)` 隐藏（需配合汉堡按钮展开，当前版本简化处理）；内容区 padding 降至 14px；顶栏 padding 降至 8px 12px；顶栏 chip 组隐藏（`display:none`）；`.col` 最小宽变 100% 单列堆叠 |
| 默认（>768px） | 侧栏 210px 固定，内容区 max-width 1200px 居中，`.col` 最小宽 380px 双列 |

个人版引导页为单卡片居中布局，`max-width:44rem`，无响应式断点需求。

---

## 第5章 交互规范

### 5.1 加载状态

#### 5.1.1 按钮加载

按钮在异步操作期间 disabled + 文案切换为「…中」（走 i18n，如 `login.loading` → 「登录中…」），避免用户重复点击。

#### 5.1.2 数据轮询

App.vue 在已登录态启动两类数据刷新：

- **轮询兜底**：设备列表 5s、告警列表 10s 间隔轮询。
- **SSE 实时推送**：连接 `/api/v1/events/stream`，事件到达即刷新对应 store（`device_online`/`device_offline`/`alert_new`/`task_status` 等），SSE 正常时暂停轮询，断线时恢复轮询兜底。
- **可见性控制**：`document.hidden` 时暂停所有轮询与 SSE，可见时恢复，避免后台标签页空耗。

#### 5.1.3 路由懒加载

所有 View 组件通过 `() => import('@/views/XxxView.vue')` 懒加载，首次访问时显示路由切换淡入动画，避免白屏。

### 5.2 空状态

空状态统一由 `DataTable` 的 `emptyText` 或 i18n `common.no_data` 处理，渲染整行 `colspan` 占位 + `--text-3` 居中文字。`RelationGraph` 在无节点时显示 `cmdb.graph_empty` 文案。

### 5.3 错误状态

#### 5.3.1 表单错误

表单错误以 `.msg.err`（`--fail` 色）显示在表单下方，`white-space:pre-wrap` 支持多行。错误文案走 i18n（如 `login.invalid_credentials`）。

#### 5.3.2 API 错误

API 调用失败时在页面顶部或卡片内显示错误消息，使用 `.msg.err` 样式。401 未授权由路由守卫统一处理，重定向到登录页。

### 5.4 确认操作

所有破坏性操作（删除设备、删除用户、强制下线）必须通过 `ConfirmModal` 二次确认：

- 普通确认：显示「取消 + 确认」两按钮，文案明确说明后果（如「此操作不可撤销」）。
- 信息提示：`info` 模式仅「确定」按钮，用于不可逆操作后的结果告知。

禁止使用原生 `confirm()` / `alert()` / `prompt()`，统一走组件保证视觉一致与无障碍。

### 5.5 反馈提示

操作成功后通过 `.msg.ok`（`--ok` 色）显示成功消息，或通过 `ConfirmModal` info 模式弹窗告知。消息区 `margin-top:10px`、`white-space:pre-wrap` 支持多行换行。

---

## 第6章 主题切换

### 6.1 机制

主题通过 `data-theme` 属性 + CSS 变量实现：

1. `tokens.css` 在 `:root` 定义 light 主题 token，在 `[data-theme="dark"]` 选择器下覆盖 dark 主题 token。
2. `useThemeStore`（Pinia）管理当前主题，`toggle()` 切换并调用 `apply(theme)` 设置 `document.documentElement.setAttribute('data-theme', theme)`。
3. 主题选择持久化到 `localStorage` key `opsmesh-theme`，初始化时读取，默认 light。
4. `main.js` 在 `app.mount('#app')` 前调用 `useThemeStore().init()`，避免首屏闪烁（FOUC）。

### 6.2 切换按钮

顶栏右侧 icon-btn，根据当前主题显示对应图标（dark 态显示 `theme-light` 图标提示切换到浅色，反之亦然），`title` 属性走 i18n（`topbar.theme_light` / `topbar.theme_dark`）提供无障碍 tooltip。

### 6.3 dark 主题设计要点

- **非纯黑**：背景采用深蓝灰 `#0f1424`，与 `--surface` 层次分明，避免 OLED 烧屏与视觉疲劳。
- **主色降亮**：`--accent` 从 `#5b5ef0` 调到 `#7c81f5`，保证暗背景上对比度。
- **状态色提亮**：`--ok` 从 `#059669` 调到 `#34d399`，`--fail` 从 `#e11d48` 调到 `#fb7185`，保证暗背景可读性。
- **阴影减弱**：dark 主题阴影透明度提高（`rgba(0,0,0,.3/.4)`），避免暗背景上阴影过重。
- **玻璃质感**：`--appbar-bg` 改为 `rgba(26,33,56,.86)`，保持 blur 效果。

---

## 第7章 国际化

### 7.1 实现方案

i18n 采用自研轻量模块（`src/i18n/index.js`），不依赖 vue-i18n，避免额外依赖与 bundle 体积：

- **语言资源**：`zh.json`（782 行）+ `en.json`，按业务域分组（`app`/`nav`/`topbar`/`login`/`register`/`overview`/`users`/`common`/`cmdb` 等）。
- **响应式当前语言**：`currentLang = ref(localStorage.getItem('opsmesh-lang') || 'zh')`。
- **翻译函数**：`t(key, params?)` 按点分路径取嵌套值，支持 `{name}` 插值。
- **回退机制**：当前语言缺少某键时回退到 `FALLBACK_LANG='zh'`，仍找不到返回 key 本身（便于排查缺失翻译）。
- **持久化**：`setLang(lang)` 写入 `localStorage` + 设置 `document.documentElement.setAttribute('data-lang', lang)`。
- **全局注入**：`main.js` 将 `t` 挂到 `app.config.globalProperties.$t`，模板内直接用 `$t('key')`。

### 7.2 切换按钮

顶栏右侧 icon-btn + 文字标签（中文态显示「中」，英文态显示「EN」），点击切换 `zh ↔ en`。

### 7.3 时间格式化

`useFormatTime` composable 根据当前语言选择 locale（zh → `zh-CN`，en → `en-US`），通过 `Intl.DateTimeFormat` 格式化为 `YYYY/MM/DD HH:mm` 24 小时制，空值返回占位符。

### 7.4 路由标题

`router.afterEach` 钩子根据 `to.meta.title` 设置 `document.title`，格式 `${title} · OpsMesh 企业版`。`meta.title` 当前为中文硬编码，未走 i18n（因 document.title 在路由守卫中同步设置，i18n 切换时不自动更新，待后续优化）。

---

## 第8章 无障碍

### 8.1 ARIA

- **图标**：`Icon.vue` 根 `<svg>` 设置 `aria-hidden="true"`，装饰性图标不朗读，避免屏幕阅读器冗余。
- **弹窗**：`ConfirmModal` / `PromptModal` 通过 `<Teleport to="body">` 渲染到 body 末尾，避免被父级 `aria-hidden` 继承遮蔽。
- **状态徽章**：`StatusBadge` 文字本身即语义，无需额外 `aria-label`。

### 8.2 键盘导航

#### 8.2.1 弹窗 ESC 关闭

`ConfirmModal`（非 info 模式）与 `PromptModal` 监听 `window.keydown`，ESC 触发 cancel 关闭。监听在 `modelValue` 变为 true 时添加、false 时移除，避免泄漏。

#### 8.2.2 输入框回车确认

`PromptModal` 输入框 `@keyup.enter="onConfirm"`，回车即确认提交，符合表单习惯。

#### 8.2.3 焦点管理

`PromptModal` 打开时通过 `nextTick` + `inputRef.focus()` + `inputRef.select()` 自动聚焦并全选默认值，用户可直接输入覆盖或编辑。

#### 8.2.4 按钮 disabled 语义

按钮 disabled 态 `opacity:.55` + `cursor:not-allowed`，键盘 Tab 跳过 disabled 按钮（浏览器原生行为）。

### 8.3 对比度

色彩体系按 WCAG AA 标准设计：

- 主文字 `--text` on `--bg`：light `#1f2540` on `#e8ecf7` 对比度 ≈ 13:1，dark `#e6ebf5` on `#0f1424` 对比度 ≈ 14:1，均远超 AA 4.5:1。
- 次要文字 `--text-2` on `--surface`：light ≈ 7:1，dark ≈ 8:1。
- 状态徽章文字 on 状态底：`--ok` on `--ok-bg` 等，light 主题对比度 ≥ 4.5:1，dark 主题通过提亮状态色保证。
- 链接 `--accent` on `--bg`：light ≈ 5:1，dark ≈ 6:1。

### 8.4 焦点可见性

输入框 focus 态通过 `border-color:var(--accent)` + `box-shadow:0 0 0 3px var(--accent-soft)` 光环明确标识焦点位置，键盘用户可清晰感知。按钮依赖浏览器原生 `:focus` outline（未全局移除）。

---

## 第9章 双前端共享设计 token

### 9.1 共享约定

个人版与企业版虽技术栈不同，但共享以下设计约定，保证跳转前后视觉连续：

表：共享设计 token

| 维度 | 个人版（app.css 内联） | 企业版（tokens.css） | 一致性 |
|---|---|---|---|
| 主色 | `--accent:#4f46e5` | `--indigo:#6366f1` / `--accent:#5b5ef0` | 同靛蓝系，企业版略调亮以适配多层表面 |
| 字体栈 | `system-ui,-apple-system,"Segoe UI",Roboto,...` | 同左 + `"PingFang SC","Microsoft YaHei"` | 同源，企业版补充中文字体 |
| 圆角 | `12px`（卡片）/ `8px`（按钮） | `--radius:14px` / `--radius-sm:9px` | 同区间，企业版略大以适配高密度 |
| 阴影 | `0 6px 24px rgba(15,23,42,.06)` | `0 1px 2px ... 0 6px 20px ...` | 同低透明度柔和浮起 |
| 文字色 | `--text:#1f2329` / `--text2:#6b7280` | `--text:#1f2540` / `--text-2:#525a78` | 同深灰系 |
| 背景 | `--bg:#f5f7fb` | `--bg:#e8ecf7` | 同浅灰蓝 |

### 9.2 个人版引导页

个人版 `index.html` 为单卡片居中布局，仅承担「跳转引导」职责：

- 卡片：`max-width:44rem`、`border-radius:12px`、`padding:2.5rem 3rem`、`box-shadow:0 6px 24px rgba(15,23,42,.06)`。
- 内容：OpsMesh badge + h1 标题 + 说明文字 + 跳转按钮 + 活跃告警提示 + 部署指引链接 + 控制面运行状态。
- 跳转：`<meta http-equiv="refresh" content="3; url=/enterprise/">` 3 秒自动跳转 + `main.js` ES module 兜底 `window.location.replace('/enterprise/')`。

### 9.3 演进方向

个人版原完整 UI 套件（约 1.3 万行原生 ES module）已于 v0.4.0 移除，仅保留引导页所需最小样式（19 行 CSS + 12 行 JS + 6 行 theme stub）。后续所有业务功能迭代集中到企业版 Vue 3 应用，个人版仅作为「根路径引导」存在。

---

## 第10章 设计交付物

### 10.1 设计 token 文件

- `web/enterprise/src/assets/tokens.css`：完整 light + dark token 定义（182 行），为设计系统的唯一真源。
- `internal/controlplane/web/assets/app.css`：个人版最小样式（19 行），与 token 同源但内联简化。

### 10.2 组件库清单

10 个自研组件 + 配套单元测试，位于 `web/enterprise/src/components/`。所有组件：

- 使用 Vue 3 `<script setup>` 语法。
- 样式 `<style scoped>` 隔离，通过 `:deep()` 穿透插槽内容。
- 通过 `defineProps` + `defineEmits` 声明接口，无运行时 props 校验开销（生产模式剔除）。
- 配套 `.test.js` 使用 Vitest + @vue/test-utils，覆盖核心交互与边界条件。

### 10.3 i18n 资源

- `web/enterprise/src/i18n/zh.json`（782 行）：中文基准语言，覆盖最全。
- `web/enterprise/src/i18n/en.json`：英文翻译，缺失键回退到 zh。
- `web/enterprise/src/i18n/index.js`：自研 i18n 模块（61 行），无 vue-i18n 依赖。

### 10.4 主题 store

- `web/enterprise/src/stores/theme.js`：Pinia store，管理 light/dark 切换 + localStorage 持久化 + DOM 属性同步。

### 10.5 验证结果

| 项 | 状态 | 说明 |
|---|---|---|
| 设计 token 完整性 | ✅ | light + dark 双主题，覆盖背景/表面/文字/主色/功能色/状态色/圆角/阴影/间距/字号/字体 |
| 组件库覆盖度 | ✅ | 10 个组件覆盖按钮/卡片/表格/表单/对话框/抽屉/导航/图标/图表/徽章/分页/关系图谱 |
| 主题切换 | ✅ | light/dark 运行时切换 + localStorage 持久化 + 首屏防闪烁 |
| i18n | ✅ | zh/en 双语 + 回退机制 + 持久化 + 全局 $t 注入 |
| 无障碍 | ✅ | ARIA hidden + ESC 关闭 + 焦点管理 + 对比度 AA |
| 响应式 | ⚠️ | 仅 768px 单断点，移动端侧栏展开未完整实现（当前版本简化处理） |
| 双前端 token 共享 | ✅ | 个人版与企业版主色/字体/圆角/阴影同源，跳转视觉连续 |

### 10.6 需要确认

1. **移动端侧栏展开**：当前 768px 断点下侧栏 `translateX(-100%)` 隐藏，但未实现汉堡按钮展开交互。是否需要补充移动端抽屉式侧栏？
2. **路由标题 i18n**：`router.meta.title` 当前为中文硬编码，i18n 切换语言时 `document.title` 不自动更新。是否需要改为 i18n key？
3. **暗色主题首屏**：`main.js` 在 mount 前调用 `themeStore.init()` 读取 localStorage 应用主题，但 CSS 首次加载时仍为 light 默认值。若用户偏好 dark，首帧仍会闪一下 light。是否需要在 `index.html` 内联一段同步脚本提前应用 `data-theme`？
4. **`aria-label` 补充**：icon-btn（主题/语言/退出）仅有 `title` tooltip，未设置 `aria-label`。屏幕阅读器在 icon-only 按钮上可能朗读不友好，是否需要补充 `aria-label`？

---

> 本文档基于 `web/enterprise/src/` 与 `internal/controlplane/web/` 源码编写，所有 token 值、组件 props、样式参数均来自实际代码。如需调整设计系统，请先修改 `tokens.css` 并同步更新本文档。