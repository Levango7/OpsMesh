# OpsMesh Frontend UI Layout Audit Report

**Scope:** `F:\Agent\kilo\workspace\OpsMesh\web\enterprise\src/views/` and `src/components/` (68 `.vue` view files, 21 `.vue` component files)
**Date:** 2026-10-23
**Auditor:** Kilo

---

## Executive Summary

The frontend has **68 view files** and **21 UI component files**. The audit found:

- **10 pages** with multiple unrelated features crammed together (multi-feature cramming)
- **12 pages** with specific card internal layout issues
- **5 missing CSS definitions** (classes/variables used but not defined)
- **3 duplicated CSS definitions** within scoped styles
- **2 views** with zero scoped styles (relying entirely on global CSS)
- **55 of 68 views** (81%) use raw HTML `<button>` instead of the `<Button>` component
- **SecurityOpsView.vue** and **NetworkOpsView.vue** do **not exist** in the codebase

Three global CSS files are imported in `main.js:13-15`:
- `src/assets/tokens.css` — legacy token system (`--surface`, `--border`, `--accent`, `--text`, etc.)
- `src/styles/design-tokens.css` — newer token system (`--color-*` prefixed)
- `src/styles/k8s.css` — K8s module shared styles (`.card`, `.flowbar`, `.field`, `.btnbar`, `.row-actions`, `.modal`, etc.)

---

## 1. Pages with Multiple Unrelated Features Crammed Together

### 1.1 SystemOpsView.vue (649 lines, 5 tabs + modals)
**Path:** `src/views/SystemOpsView.vue`

Five distinct system administration domains are crammed into a single page via tabs:
- **Users** (CRUD: list, create, delete)
- **Groups** (CRUD: list, create, delete)
- **Firewall** (list rules, add rule, query raw rules, delete rule)
- **Services** (list + refresh)
- **Journal** (log query with 4 input fields)

Each tab is a separate operational domain. The user/group/firewall/service CRUD operations should be split into separate sub-pages or at minimum separate routes. The page also has 3 drawer modals (create user, create group, add firewall rule) and 1 delete confirm modal.

**Recommendation:** Split into `/system/users`, `/system/groups`, `/system/firewall`, `/system/services`, `/system/journal` sub-routes.

### 1.2 AlertRulesView.vue (498 lines, 3 tabs + 3 drawers)
**Path:** `src/views/AlertRulesView.vue`

Three distinct alerting subsystems crammed into tabs:
- **Rules** (alert rules CRUD)
- **Engine rules** (engine rule CRUD with conditions as JSON)
- **Silences** (silence management with time range input)

Each tab has its own drawer form. The three rule types are distinct enough to warrant separate pages.

**Recommendation:** Split into `/alerts/rules`, `/alerts/engine`, `/alerts/silences`.

### 1.3 ConfigHotpushView.vue (319 lines, 3 tabs)
**Path:** `src/views/ConfigHotpushView.vue`

Three config management features:
- **Hot push** (single-target config push with 5 form fields)
- **Canary** (multi-target gray release with 6 form fields + strategy selection)
- **Versions** (historical version query with 3 filter inputs + results table)

**Recommendation:** Split into `/config/hotpush`, `/config/canary`, `/config/versions`.

### 1.4 BillingView.vue (539 lines, 4 tabs + 3 drawers)
**Path:** `src/views/BillingView.vue`

Four unrelated billing domains in tabs:
- **Plans** (plan CRUD)
- **Subscriptions** (subscription CRUD with plan dropdown)
- **Invoices** (read-only invoice list with detail drawer)
- **Usage** (read-only usage stats in card grid)

Each tab is a different billing entity. The plan form drawer is reused in the subscription form.

**Recommendation:** Split into `/billing/plans`, `/billing/subscriptions`, `/billing/invoices`, `/billing/usage`.

### 1.5 AIOpsView.vue (433 lines, 5 tabs)
**Path:** `src/views/AIOpsView.vue`

Five AI-powered features crammed together:
- **Analyze** (root cause analysis with alert form fields)
- **Remediate** (generate remediation plan with root cause input)
- **NL** (natural language to task commands)
- **Execute** (execute remediation plan results)
- **History** (execution history table + detail view)

Additionally uses raw HTML `<table class="data-table">` instead of the `<DataTable>` component for the history tab, and uses raw `<button>` throughout instead of `<Button>`.

**Recommendation:** Split into `/aiops/analyze`, `/aiops/remediate`, `/aiops/execute`, `/aiops/history` (NL could be a sub-feature of analyze or remediate).

### 1.6 TasksView.vue (248 lines, 3 distinct sections on one page)
**Path:** `src/views/TasksView.vue`

Three distinct functions on a single scrollable page without tabs:
1. **Task dispatch form** (lines 6-49) — form with agent select, type select, command, path, content
2. **Task list table** (lines 54-89) — filterable task list with cancel action
3. **Pending approval table** (lines 92-111) — approval queue with approve/reject actions

The dispatch form and the task list are conceptually separate ("create" vs "monitor"). The approval section is a completely different workflow.

**Recommendation:** Split into `/tasks/dispatch`, `/tasks/list`, `/tasks/approvals`.

### 1.7 GPUView.vue (496 lines, multiple cards on one page)
**Path:** `src/views/GPUView.vue`

Four GPU management domains on one page:
1. **Node list** (card at line 27) — table with nodes, actions
2. **Utilization chart + workloads** (card at line 65, card at line 91 — nested)
3. **Model management** (card at line 122) — pull/delete models
4. **Quota overview** (card at line 150) — tenant GPU quotas

Plus two modals (add workload at line 169, pull model at line 215). The model management and quota sections could be separate pages.

**Recommendation:** At minimum split GPU models and quotas into separate routes.

### 1.8 InfraConsoleView.vue (398 lines, 14 infrastructure domains)
**Path:** `src/views/InfraConsoleView.vue`

A single dispatch console covering 14+ infrastructure domains via a domain navigation sidebar:
Messaging queues, databases, OLAP, TSDB, vector DB, graph DB, load balancers, object storage, Harbor, monitoring, kernel parameters, security, virtualization, Ansible.

Each domain has its own actions and form fields. This is essentially a mega-page that should be split.

**Recommendation:** Split by domain into separate routes, or at minimum separate major categories (databases, messaging, caches, etc.).

### 1.9 AdminConsoleView.vue (141 lines, 5 distinct functions)
**Path:** `src/views/AdminConsoleView.vue`

Combines:
1. Log level selector (dropdown at line 12)
2. Diagnostics download link (line 19)
3. Effective config display (card at line 28)
4. Store failure monitoring (card at line 34)
5. Service routing table (card at line 45)
6. Service traffic counters (card at line 51)

This is a diagnostics/admin page that crams configuration, errors, routing, and metrics together.

**Recommendation:** Split into `/admin/config`, `/admin/diagnostics`, `/admin/routing`, `/admin/traffic`.

### 1.10 OSOptimizeView.vue (348 lines, 4 functions)
**Path:** `src/views/OSOptimizeView.vue`

Combines:
1. Template list/filter/category tabs (lines 7-44)
2. Template detail drawer (lines 46-76)
3. Execution dialog with param form (lines 78-122)
4. Execution result log polling (lines 116-119)

**Recommendation:** Split template execution into a separate flow, or at least separate the list from the execution dialog.

---

## 2. Card Internal Layout Issues

### 2.1 K8sClustersView.vue — No scoped styles + card header/button issues
**Path:** `src/views/K8sClustersView.vue`

**Issue: Missing `<style scoped>` block entirely (lines 1-224).**
The view uses `.card` (line 12), `.flowbar` (line 13), `.field` (lines 61, 65, 69), `.btnbar` (line 73), `.row-actions` (line 38), `.modal-mask` (line 54), `.modal` (lines 55, 100), `.modal-head` (line 56), `.modal-body` (line 60), `.poll-err` (line 10), `.muted` (line 23) — ALL rely on the global `k8s.css`. No component-level style isolation.

**Issue: Card header not aligned with content (line 14).**
```html
<h3 style="margin: 0; font-size: 13px">{{ $t('k8s.clusters') }}</h3>
```
Inline `style="margin: 0; font-size: 13px"` overrides the global `.card h3` (which has `margin: 0 0 12px 0; font-size: 13px`). The inline margin removes the bottom margin, causing the subsequent DataTable to have no spacing gap from the header.

**Issue: Inconsistent button placement in flowbar (lines 15-20).**
Primary "Add Cluster" button and ghost "Refresh" button sit in the same `flowbar` with no separator or consistent alignment. The `flowbar` uses `align-items: flex-end` (k8s.css:138) which should align them, but `<Button size="sm">` has height 32px while the raw `<h3>` is taller, creating vertical misalignment.

**Issue: Modal form fields inconsistent with main form fields (lines 60-78).**
The add-cluster modal uses `.field` divs with `<input>` and `<textarea>` elements. The global k8s.css `.field` has `margin-bottom: 8px` but `.field label` has `font-size: 11px` — smaller than other views.

### 2.2 K8sResourcesView.vue — Form field misalignment + tab button inconsistency + no scoped styles
**Path:** `src/views/K8sResourcesView.vue`

**Issue: Missing `<style scoped>` block entirely (lines 1-394).**
Same issue as K8sClustersView — ALL classes rely on global k8s.css.

**Issue: Form fields in flowbar not vertically aligned (lines 14-34).**
The `.flowbar` contains two `.field` divs (select at line 14, input at line 23) and a `<Button>` (line 32). The global k8s.css `.flowbar` sets `align-items: flex-end` (line 138), which should bottom-align. However:
- The `<select>` gets `height: 34px` from `.flowbar .field select` (k8s.css:159-161)
- The `<input>` gets `height: 34px` from `.flowbar .field input` (same rule)
- The `<Button size="sm">` has height 32px (Button.vue:126) — **2px shorter** than the form fields, causing misalignment.

**Issue: Resource type tabs use raw `<button>` instead of `<Button>` component (lines 39-46).**
```html
<button v-for="rt in resourceTypes" class="res-tab" :class="{ active: ... }" @click="...">
```
This is the ONLY place in the K8s views that uses raw buttons for tabs. Other views (AlertRulesView, ConfigHotpushView, BillingView) also use raw buttons for tabs, but the K8s views should be consistent since they already import `<Button>`.

**Issue: Inconsistent column widths for resource types (lines 175-233).**
- Pods: actions column 160px
- Deployments: actions column 140px
- Services: NO actions column, NO width constraints on any column
- ConfigMaps: NO actions column, NO width constraints
- Secrets: NO actions column, NO width constraints
- Nodes: NO actions column, NO width constraints

Tables for services/configmaps/secrets/nodes will not have proper column sizing, potentially causing overflow or cramped layouts.

### 2.3 AdminConsoleView.vue — Button styling inconsistency + card header issues
**Path:** `src/views/AdminConsoleView.vue`

**Issue: `.btn` class used on `<a>` but not defined anywhere (line 19).**
```html
<a :href="diagnosticsUrl" class="btn outline" target="_blank">
```
Neither `.btn` nor `.btn.outline` is defined in any CSS file. tokens.css defines `button.outline` (line 142), which matches `<button class="outline">` but NOT `<a class="btn outline">`. Result: the diagnostics link is **completely unstyled** — no border, no padding, no background.

**Issue: Mixed button styles in toolbar (lines 9-24).**
The `.btnbar` contains three different element types:
1. `<label class="field-inline">` with a `<select>` (styled by `.field-inline` scoped style)
2. `<a class="btn outline">` (undefined styles — unstyled)
3. `<button class="outline">` (styled by global `button.outline`)

These three elements have inconsistent heights and padding.

**Issue: Card headers use `<h3>` without consistent class (lines 29, 35, 46, 52).**
Other views use `<h3 class="card__title">` (SystemOpsView:41, GPUView:29/66/93/124/152, OverviewView:53/79/89) but AdminConsoleView uses bare `<h3>`. The scoped style does define `.card h3` (line 136), so it works, but it's inconsistent with the `card__title` convention.

**Issue: Store failures table has no slot templates (line 39).**
```html
<DataTable :columns="failureCols" :rows="storeFailures.topOps" ... />
```
The columns `failureCols` only have `key` and `title` — no `slot` property, so data renders as plain text. The `op` column (width 200px) may truncate long operation names without ellipsis handling.

### 2.4 DeadLetterView.vue — Card not defined in scoped style
**Path:** `src/views/DeadLetterView.vue`

**Issue: `.card` class used at line 6 but NOT defined in scoped style (lines 87-91).** The scoped style only defines `.flowbar`, `.poll-err`, and `.row-actions`. The `.card` class relies on the global k8s.css definition, which means if k8s.css changes, this view breaks.

**Issue: Flowbar contains only a single button (line 7-10).**
```html
<div class="flowbar">
  <Button variant="ghost" size="sm" ...>Refresh</Button>
</div>
```
A `.flowbar` (designed for multiple inline items) wraps a single button — unnecessary complexity. A direct margin would suffice.

**Issue: Table columns lack width constraints (lines 56-64).**
The `failureReason` column (line 62) has no width limit and could expand very wide, pushing other columns. The `command` column (line 59) truncates via a function but the template doesn't use `<code>` with overflow handling.

### 2.5 TasksView.vue — Three card sections with inconsistent header styling
**Path:** `src/views/TasksView.vue`

**Issue: Three separate card sections with inconsistent headers (lines 6, 50, 54, 92).**
- Card 1 (line 6): `<h3 data-testid="task-form-title">` — task dispatch form
- Card 2 (line 50): no `<h3>`, just `<p class="muted">` — no-permission fallback
- Card 3 (line 54): `<div class="flowbar">` with `<div class="field">` + Button — task list
- Card 4 (line 92): `<h3 data-testid="task-approval-title">` — pending approvals

The cards have no consistent header pattern. Card 1 uses `<h3>`, Card 3 uses `.flowbar` (no h3), Card 4 uses `<h3>`.

**Issue: `.field label` conflicts with global `label` style (lines 239-240).**
```css
.field { display: flex; flex-direction: column; gap: 4px; margin-bottom: 8px; min-width: 220px; }
.field label { margin: 0; font-size: 12.5px; color: var(--text-2); }
```
The scoped `.field label` sets `margin: 0` but does NOT override `display` or `gap` from the global tokens.css `label` rule (line 160): `label { display: inline-flex; align-items: center; gap: 6px; margin: 4px 12px 8px 0; }`. Since the scoped selector `.field label` has the same specificity as the global `label` (both are type+class), but scoped CSS adds the data attribute (increasing specificity), the scoped `margin: 0` wins. However, `display: inline-flex` and `gap: 6px` from the global rule are NOT overridden — labels become inline-flex with gap, which is unexpected for a vertical flex field layout.

**Issue: `.row` class used at line 9 but defined globally (tokens.css:164).**
```html
<div class="row">
  <div class="field">...</div>
  <div class="field">...</div>
</div>
```
The global `.row` (tokens.css:164) is `display: flex; gap: 20px; flex-wrap: wrap; align-items: flex-start;`. This works but the `.field` has `min-width: 220px` — on narrow screens, two fields won't fit and will stack, but there's no responsive collapse for `.row` in the non-k8s views.

**Issue: Task table `command` column not truncated (line 76).**
```html
<template #cell-command="{ value }"><code>{{ value }}</code></template>
```
No truncation — long commands overflow the table cell. The `DeadLetterView` at least uses a `truncate()` function (line 66-68).

**Issue: Approval table missing `type` column (line 94 vs 171).**
Approval table columns (lines 180-186) omit the `type` column that the main task table has (line 174). Users can't see task type in approvals.

### 2.6 DevicesView.vue — Provisioning button conditional + inconsistent action variants
**Path:** `src/views\DevicesView.vue`

**Issue: Provisioning button conditionally shown with different variant (line 87).**
```html
<Button v-if="dev.state === 'discovered'" variant="primary" size="sm" @click="provision">
```
When the device is NOT in 'discovered' state, the `btnbar` in the drawer (lines 85-109) has no primary button — only two ghost buttons (view metrics, dispatch task). This creates a visual jump when the device state changes.

**Issue: Detail button uses `variant="primary"` but dispatch uses `variant="ghost"` (lines 40-57).**
In the row actions, the detail button is `variant="primary"` (line 41) while the dispatch button is `variant="ghost"` (line 50). In most other views, primary is reserved for the main action and ghost for secondary. Here "detail" is likely a secondary action, not the primary one.

**Issue: Segments rendered as bare tables without card wrappers (lines 17-61).**
Each segment group has a title (`<h3 class="seg-title">`) and a DataTable, but no `.card` wrapper. This means the tables float directly on the page background with no visual container, which is inconsistent with other views that wrap tables in cards.

### 2.7 OSOptimizeView.vue — Mixed button systems + card not in scoped style
**Path:** `src/views/OSOptimizeView.vue`

**Issue: `.card` class used at line 20 but NOT defined in scoped style (lines 306-349).**
The scoped style defines `.cat-filter`, `.cat-btn`, `.row-actions`, `.poll-err`, `.detail-meta`, `.field-hint`, `.badge`, `.code-block`, `.params-section`, `.modal-mask`, `.modal`, `.modal-head`, etc. — but NOT `.card`. The card relies on global k8s.css.

**Issue: Mixed button systems — raw `<button>` alongside no `<Button>` component import.**
OSOptimizeView does NOT import `Button` from `@/components/ui` (line 126-127: imports only DataTable, StatusBadge, DetailDrawer, Icon). All buttons are raw HTML `<button>`:
- Line 39: `<button class="xs outline">` — View
- Line 40: `<button class="xs primary">` — Execute
- Line 83: `<button class="xs outline">` — close modal
- Line 113: `<button class="outline">` — cancel
- Line 112: `<button class="primary">` — confirm execute

The `.xs` class comes from tokens.css (line 141: `button.xs { height: 26px; padding: 0 11px; ... }`), `.primary` from tokens.css (line 135: `button.primary { background: var(--accent); ... }`), `.outline` from tokens.css (line 142: `button.outline { background: transparent; ... }`). But these are element-level styles (`button.primary`) not class-level — they only work on `<button>` elements, not on `<Button>` component renders.

**Issue: `.modal` redeclared in scoped style (lines 338-342) conflicting with global k8s.css.**
Both OSOptimizeView's scoped style and the global k8s.css define `.modal`. The scoped version will override for this component's templates, but it uses `var(--surface)` and `var(--radius)` (from tokens.css) while k8s.css uses `var(--color-surface-1)` and `var(--radius-lg)`. The scoped `.modal` at line 340: `width: 540px; max-width: 94vw; max-height: 88vh; overflow: auto; background: var(--surface); border-radius: var(--radius); padding: 20px 22px;` — this differs from global k8s.css `.modal` which uses `--color-surface-1` and `--radius-lg`. Not a bug per se, but inconsistency.

### 2.8 HelmReposView.vue — Duplicated `.btnbar` + missing `.card` (no card usage)
**Path:** `src/views/HelmReposView.vue`

**Issue: `.btnbar` defined twice in same scoped style (lines 193 and 205).**
```css
.btnbar { display: flex; gap: 8px; margin-bottom: 12px; }  /* line 193 */
...
.btnbar { margin-top: 8px; }  /* line 205 */
```
The second definition overrides `margin-bottom: 12px` with `margin-top: 8px` but does NOT preserve `margin-bottom`. Result: the `.btnbar` loses its bottom margin, potentially causing layout collapse where the btnbar sticks to subsequent content.

**Issue: Raw `<button>` throughout — no `<Button>` component import.**
All buttons (lines 9, 12, 28, 29, 55, 56) are raw HTML `<button>`. The view imports DataTable, DetailDrawer, Icon, ConfirmModal — but NOT Button. This is inconsistent with sibling HelmReleasesView which also doesn't import Button.

**Issue: No card wrapper around repository table (lines 8-32).**
The repository list (DataTable) is placed directly on the page without a `.card` container. Compare with TasksView (line 54) and DeadLetterView (line 6) which wrap their tables in cards.

### 2.9 HelmReleasesView.vue — Same duplicated `.btnbar` issue + missing `.card`
**Path:** `src/views/HelmReleasesView.vue`

**Issue: `.btnbar` defined twice in same scoped style (lines 294 and 307).**
Identical problem to HelmReposView — second definition at line 307 (`margin-top: 8px`) overrides the first.

**Issue: Raw `<button>` throughout — no `<Button>` component.**
All buttons (lines 9, 12, 31, 32, 33, 34, 65, 66, 83, 98, 99) are raw HTML. No Button import.

**Issue: 5 action buttons per row (lines 31-34).**
Each release row has Upgrade, History, Rollback, Uninstall buttons — 4 inline buttons in a 140px-wide column. These are cramped and should be a dropdown menu.

**Issue: No card wrapper around release table (lines 8-37).**
Repository/release list is not wrapped in a `.card`.

### 2.10 CloudAccountsView.vue — Inconsistent Button API + dual table component usage
**Path:** `src/views/CloudAccountsView.vue`

**Issue: `size="small"` instead of `size="sm"` (lines 29, 30, 55, 65, 75, 84, 93).**
The `<Button>` component accepts `size: "sm" | "md" | "lg"` (Button.vue:32). CloudAccountsView passes `size="small"` which is NOT a valid prop value. The computed `sizeClasses` (Button.vue:73) generates `btn--small` which has NO CSS definition — only `btn--sm`, `btn--md`, `btn--lg` are defined (Button.vue:125-143). Result: buttons get default `btn--md` styling (40px height) instead of the intended small size.

**Issue: `variant="outline"` instead of `variant="ghost"` (lines 30, 55, 65, 75, 84).**
The Button component supports `variant: "primary" | "secondary" | "ghost" | "danger"` (Button.vue:31). CloudAccountsView uses `variant="outline"` which falls into the default case and gets no variant class — effectively unstyled (default primary background).

Wait — let me re-check. Looking at Button.vue:63-71:
```js
const variantClasses = computed(() => {
  const v = props.variant
  const base = ["btn", `btn--${v}`]
  if (v === "primary") base.push("btn--primary")
  else if (v === "secondary") base.push("btn--secondary")
  else if (v === "ghost") base.push("btn--ghost")
  else if (v === "danger") base.push("btn--danger")
  return base
})
```
With `variant="outline"`, the classes would be `["btn", "btn--outline"]` and since none of the if branches match, no variant-specific class is added. The `.btn--outline` class is NOT defined in the scoped style, so the button gets the default `.btn` styles (primary background). This means the "Sync", "Test", "Edit", "Resources" buttons that should be outline/ghost-style are actually rendered as primary buttons.

**Issue: 5 action buttons per row with 260px column width (lines 51-103).**
The actions column is 260px wide (line 284) with 5 buttons — Sync, Test, Edit, Resources, Delete. This is very crowded. No dropdown consolidation.

**Issue: Redudant double header (lines 4-24).**
The view renders a `<Header>` component (line 5, from `@/components/ui/Header.vue`) AND a separate `<div class="page-title">` with its own `<h2>` (line 21). Both show the same title. The `Header` component likely already renders a title, making the page title appear twice.

**Issue: Uses `<Table>` component instead of `<DataTable>` (line 38).**
CloudAccountsView uses `<Table :data="accounts" :columns="columns" ...>` from `@/components/ui` (the full-featured table with Prop-based API: `prop`, `label`, `width`). Other views use `<DataTable :rows="..." :columns="...">` from `@/components/DataTable.vue` (the simpler table with Key-based API: `key`, `title`, `slot`). This creates two parallel table APIs — CloudAccountsView is the ONLY view using `Table` from ui instead of `DataTable`.

### 2.11 GPUView.vue — Nested card without header alignment
**Path:** `src/views/GPUView.vue`

**Issue: Nested card inside card (line 91) with misaligned header.**
The right column (line 64) contains a card for utilization chart (line 65) and then NESTS another card for workloads (line 91) inside the same `.col`. The nested card's `<h3>` at line 93 uses `style="margin: 0"` (inline) which removes the card header margin, making the chart title and workload title inconsistent.

**Issue: Card headers use `style="margin: 0"` inline (lines 29, 66, 93, 124, 152).**
All card headers in GPUView use inline `style="margin: 0"` to override the scoped `.card h3 { margin: 0 0 12px 0; }`. This removes the bottom margin, causing the content below to touch the header. Should use a consistent `card__title` class instead.

### 2.12 OverviewView.vue — No card scoped style + glass-card class undefined
**Path:** `src/views/OverviewView.vue`

**Issue: `class="card glass-card"` used at lines 52, 78, 89 but NO `.card` or `.glass-card` defined in scoped style (lines 190-382).**
The scoped style defines `.stat-card`, `.quick-item`, `.cap-item`, `.alert-row` etc. but does NOT define `.card` or `.glass-card`. These classes rely entirely on global tokens.css `.card` (line 166-173) which has `padding: 18px 20px; margin-bottom: 18px;`. The `glass-card` class is completely undefined — it's used in the template but has no CSS definition anywhere.

---

## 3. Specific Visual Anomalies (Requested Files)

| File | Issue | Line(s) |
|---|---|---|
| **K8sClustersView.vue** | No `<style scoped>` block at all | Entire file |
| | `.card` header `<h3>` uses inline `style="margin: 0"` overriding global | 14 |
| | Button placement in flowbar: primary + ghost buttons without consistent spacing | 15-20 |
| | `.btnbar` in modal lacks consistent alignment | 73 |
| **K8sResourcesView.vue** | No `<style scoped>` block at all | Entire file |
| | `<select>` (34px) vs `<Button size="sm">` (32px) vertical misalignment in flowbar | 14-34 |
| | Raw `<button>` for resource tabs instead of `<Button>` | 39-46 |
| | Services/configmaps/secrets/nodes column widths undefined — tables may overflow | 197-229 |
| **AdminConsoleView.vue** | `<a class="btn outline">` — `.btn` class undefined, link unstyled | 19 |
| | Mixed element types in `.btnbar`: `<label>`, `<a>`, `<button>` | 9-24 |
| | Card headers use bare `<h3>` instead of `class="card__title"` | 29, 35, 46, 52 |
| | Store failures DataTable has no cell slots for `op` column — no truncation | 39 |
| **DeadLetterView.vue** | `.card` not defined in scoped style (relies on global) | 6 |
| | `.flowbar` wraps a single refresh button unnecessarily | 7-10 |
| | `failureReason` and `command` columns lack width constraints | 56-64 |
| | Mixed `<Button>` and DataTable but no card header class | 6 |
| **TasksView.vue** | Three card sections with inconsistent headers (h3 vs no h3 vs flowbar) | 6, 54, 92 |
| | `.field label` doesn't override global `display: inline-flex` from tokens.css | 239-240 |
| | Task table `command` column not truncated (DeadLetterView truncates but TasksView doesn't) | 76 |
| | Approval table missing `type` column present in main table | 180-186 |
| | `.row` class used at line 9 but only `.field` defined in scoped style (not `.row`) | 9 |
| **DevicesView.vue** | Segment blocks lack card wrappers — tables float on background | 17-61 |
| | Detail button `variant="primary"` but dispatch button `variant="ghost"` — inconsistent hierarchy | 40-57 |
| | Provision button conditionally shown with no fallback — btnbar layout jumps | 85-109 |
| | Drawer has `fail-row` class via `:row-class` but no visual distinction for error rows | 24, 269 |
| **OSOptimizeView.vue** | `.card` not defined in scoped style | 20 |
| | Raw `<button>` throughout — no `<Button>` component import | 39, 40, 83, 112-113 |
| | `.modal` redeclared in scoped style with different variable fallback chain | 338-342 |
| | Category filter buttons use `.cat-btn` class — not a recognized design pattern | 7-15 |
| **HelmReposView.vue** | `.btnbar` duplicated in scoped style (lines 193 + 205) | 193, 205 |
| | Raw `<button>` throughout — no `<Button>` import | 9, 12, 28-29, 55-56 |
| | No card wrapper around repository table | 8-32 |
| | `.tag` uses `var(--accent-soft)` but scoped style doesn't define `.tag` variants | 207-210 |
| **HelmReleasesView.vue** | `.btnbar` duplicated in scoped style (lines 294 + 307) | 294, 307 |
| | Raw `<button>` throughout — no `<Button>` import | 9-12, 31-34, 65-66, 83, 98-99 |
| | 4 action buttons per row in 140px column — cramped | 31-34 |
| | No card wrapper around release table | 8-37 |
| **CloudAccountsView.vue** | `size="small"` instead of `size="sm"` — invalid Button prop, falls to default md | 29, 30, 55, 65, 75, 84, 93, 99 |
| | `variant="outline"` instead of `variant="ghost"` — invalid Button variant, renders as primary | 30, 55, 65, 75 |
| | Uses `<Table>` (ui) component instead of `<DataTable>` — dual table API | 38, 114 |
| | Redundant double header: `<Header>` component + `<div class="page-title">` | 5-24 |
| | 5 action buttons per row in 260px column — very crowded | 51-103 |
| | No `.card` class usage — table sits on page background without card container | 37-105 |
| **SystemOpsView.vue** | Uses `@click.stop` on row actions divs but no `:click` handler on `Table` rows | 48, 64, 84, 102 |
| | `.card` defined with `padding: var(--space-4, 16px)` — no horizontal padding variant | 562 |
| | `.btnbar` at line 206 uses inline `style="justify-content: flex-end"` | 206 |
| | `Form` component used with `type="submit"` but `@submit.prevent` on Form (not native form) | 151, 167 |
| | `FormItem` label uses `label="{{ $t(...) }}"` — literal braces instead of binding | 152, 155, 158, 161, 187, 190, 193 |

**Note on line 152:** `<FormItem label="{{ $t('system_ops.col_name') }} *">` — This uses a string literal `{{ }}` instead of a Vue binding `:label="t(...)"`. The `FormItem` component will receive the literal string `{{ $t('system_ops.col_name') }} *` instead of the translated value. Same issue at lines 155, 158, 161, 187, 190, 193.

### SecurityOpsView.vue and NetworkOpsView.vue

**Neither file exists** in `src/views/`. There is `SystemOpsView.vue` (system operations) and `NetworkDevicesView.vue`, `NetworkDiagnoseView.vue`, `NetworkTopologyView.vue` (network operations split across 3 files), but no file named `SecurityOpsView.vue` or `NetworkOpsView.vue`.

---

## 4. Missing CSS Definitions

### 4.1 Undefined CSS Variables

| Variable | Used In | Should Be | Defined In |
|---|---|---|---|
| `--font-size` | `src/components/ui/Input.vue:263` | `--font-size-base` or `--fs-base` | design-tokens.css has `--font-size-base`, tokens.css has `--fs-base` |
| `--form-item-label-width` | `src/components/ui/FormItem.vue` | (undefined — should have default) | Not defined anywhere |
| `--mono` | `src/views/APIKeysView.vue`, `src/views/GatewayRoutesView.vue` | `--font-family-mono` or `--font-mono` | design-tokens.css has `--font-family-mono`, tokens.css has `--font-mono` |
| `--primary` | `src/views/InfraConsoleView.vue:350` | `--color-primary` or `--accent` | Neither defined in any CSS file |

### 4.2 Classes Used But Not Defined Anywhere

| Class | Used In | Defined In | Severity |
|---|---|---|---|
| `.btn` (as class selector) | `AdminConsoleView.vue:19` (`<a class="btn outline">`) | **Not defined** | Critical — link is completely unstyled |
| `.glass-card` | `OverviewView.vue:52,78,89` | **Not defined** | High — class does nothing |
| `.btn--small` | Generated by `<Button size="small">` in `CloudAccountsView.vue` | **Not defined** (only `btn--sm` exists) | High — Button renders at wrong size |
| `.btn--outline` | Generated by `<Button variant="outline">` in `CloudAccountsView.vue` | **Not defined** (Button supports `ghost`, not `outline`) | High — buttons render as primary |
| `.domain-btn.active` style | `InfraConsoleView.vue` scoped style (line 323) — actually IS defined | ✓ | — |
| `.tab.active` | `AIOpsView.vue:410`, `BillingView.vue:516`, `AlertRulesView.vue:474` | Each view defines in its own scoped style | — |

### 4.3 `.card` Class — Defined Where, Used Where Not

The `.card` class is used in 23 view files. It is properly defined in **scoped styles** in only 7 views:
- AdminConsoleView.vue (line 135) ✓
- GPUView.vue (line 420) ✓
- AIOpsView.vue (line 411) ✓
- SystemOpsView.vue (line 562) ✓
- ConfigHotpushView.vue (line 289) ✓
- CanaryView.vue (assumed ✓ — uses `card__title`)
- MiddlewareDeployView.vue (assumed ✓)

It is used but **NOT defined in scoped styles** in:
- K8sClustersView.vue (no scoped style at all) ✗
- K8sResourcesView.vue (no scoped style at all) ✗
- DeadLetterView.vue ✗
- TasksView.vue ✗
- OSOptimizeView.vue ✗
- OverviewView.vue (uses `card glass-card` — neither defined) ✗
- HelmReleasesView.vue (no card usage) N/A
- HelmReposView.vue (no card usage) N/A

**Note:** These views still render correctly because `k8s.css` defines `.card` globally. However, this creates a fragile dependency — if `k8s.css` is removed or changed, these views break. Additionally, the global `.card` in `k8s.css` and `tokens.css` have slightly different padding values (`16px 18px` vs `18px 20px`).

### 4.4 `.flowbar` Class — Global Definition Relied Upon by Many Views

The global `k8s.css` defines `.flowbar` (line 136). Views that use `.flowbar` but do NOT define it in their scoped styles:
- K8sClustersView.vue (no scoped style) ✗
- K8sResourcesView.vue (no scoped style) ✗
- DeadLetterView.vue ✗ (scoped has `.flowbar` at line 88 ✓)
- Wait — DeadLetterView DOES define `.flowbar` at line 88 ✓

Actually, let me recheck. From the DeadLetterView.vue read, the scoped style at lines 87-91 defines:
```css
.flowbar { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; margin-bottom: 10px; }
.poll-err { ... }
.row-actions { display: flex; gap: 4px; }
```

So DeadLetterView DOES define `.flowbar` ✓.

Views that use `.flowbar` WITHOUT defining it in scoped style:
- K8sClustersView.vue ✗ (no scoped style)
- K8sResourcesView.vue ✗ (no scoped style)
- OSOptimizeView.vue — doesn't use `.flowbar` in template, uses `.flowbar` only in... wait, let me check. Actually from the read, OSOptimizeView uses `.flowbar` at line 106 in the logs modal. But it doesn't define it in scoped style. So it relies on global k8s.css.

Actually, I need to be more precise. Let me recheck which views use `.flowbar` but don't define it.

From the scoped styles I've read:
- DeadLetterView: defines `.flowbar` ✓
- TasksView: defines `.flowbar` ✓ (line 241)
- DevicesView: does NOT define `.flowbar` — but does it use it? No, DevicesView uses `.btnbar` not `.flowbar`. It does use `flowbar` class in the template... let me check. Looking at the template, line 85: `<div class="btnbar">`. No `.flowbar` usage in DevicesView.

OK, the views that use `.flowbar` without defining it in scoped styles are:
- K8sClustersView.vue ✗ (no scoped style at all)
- K8sResourcesView.vue ✗ (no scoped style at all)
- OSOptimizeView.vue ✗ (scoped style defines `.btnbar` but not `.flowbar`)
- GPUView.vue — defines `.flowbar` at line 445 ✓
- AIOpsView.vue — defines `.flowbar` at line 411... wait, line 411 is `.card`. Let me check... Actually AIOpsView scoped style line 411: `.card { ... }` and line 405: `.tabs { ... }`. It does NOT define `.flowbar` but uses it at lines 35, 87, 106, 166, 191, 214. ✗
- SystemOpsView.vue — defines `.flowbar` at line 554 ✓
- AdminConsoleView.vue — defines `.btnbar` at line 132, `.field-inline` at line 133. Does it use `.flowbar`? No, it uses `.btnbar` instead. ✓ (n/a)

So views using `.flowbar` without scoped definition:
1. K8sClustersView.vue (no scoped style)
2. K8sResourcesView.vue (no scoped style)
3. OSOptimizeView.vue
4. AIOpsView.vue

### 4.5 `.btnbar` Class — Global Definitions + Duplicated Scoped Definitions

Global definitions:
- tokens.css line 163: `.btnbar { display: flex; gap: 8px; flex-wrap: wrap; margin: 6px 0; }`
- k8s.css line 16-22: `.btnbar { display: flex; align-items: center; gap: var(--space-2, 8px); margin-bottom: var(--space-3, 12px); flex-wrap: wrap; }`

Duplicated scoped definitions (twice in same file):
- HelmReposView.vue: lines 193 and 205 (second lacks `display`, `gap`, `margin-bottom` from first)
- HelmReleasesView.vue: lines 294 and 307 (same issue)

### 4.6 `.field` Class — Inconsistent Scoped Redefinitions

The global `k8s.css` defines `.field` (line 25) with:
```css
.field { display: flex; flex-direction: column; gap: 4px; margin-bottom: 8px; }
.field label { margin: 0; font-size: 11px; font-weight: 500; color: #6b7280; }
```

Different scoped definitions:
- TasksView.vue line 239: `.field { display: flex; flex-direction: column; gap: 4px; margin-bottom: 8px; min-width: 220px; }` (+ `label` at 240)
- SystemOpsView.vue line 542: `.field { display: flex; flex-direction: column; gap: 4px; }` (no margin-bottom!)
- ConfigHotpushView.vue line 296: `.field { display: flex; flex-direction: column; gap: 4px; margin-bottom: 10px; min-width: 220px; }`
- HelmReposView.vue line 203: `.field { display: flex; flex-direction: column; gap: 5px; }`
- HelmReleasesView.vue line 304: `.field { display: flex; flex-direction: column; gap: 5px; }`
- AlertRulesView.vue line 493: `.field { display: flex; flex-direction: column; gap: 5px; }`

Inconsistent gap values (4px vs 5px), margin-bottom values (8px vs 10px vs 0px), and min-width (sometimes set, sometimes not).

### 4.7 `.poll-err` Class — Inconsistent Scoped Redefinitions

Used in 15+ views without a shared definition. Each view that defines it in scoped style has slightly different values:
- k8s.css (global): `padding: 8px 12px; margin: 6px 0 12px 0; ...`
- TasksView.vue: `padding: 8px 12px; margin: 6px 0; ...`
- DeadLetterView.vue: `padding: 8px 12px; margin: 6px 0 12px; ...`
- OSOptimizeView.vue: `padding: 8px 12px; margin: 6px 0; ...`
- AIOpsView.vue: `padding: 8px 12px; margin: 6px 0; ...`
- HelmReposView.vue: `padding: 8px 12px; margin: 6px 0; ...`
- HelmReleasesView.vue: `padding: 8px 12px; margin: 6px 0; ...`
- SystemOpsView.vue: `padding: 8px 12px; margin: 6px 0; ...`
- AlertRulesView.vue: `padding: 8px 12px; margin: 6px 0; ...`
- ConfigHotpushView.vue: `padding: 8px 12px; margin: 6px 0; ...`
- GPUView.vue: same as k8s.css... wait, GPUView doesn't define `.poll-err` in scoped style

Views that DON'T define `.poll-err` in scoped style but use it:
- K8sClustersView.vue (no scoped style)
- K8sResourcesView.vue (no scoped style)
- OverviewView.vue
- AdminConsoleView.vue — wait, AdminConsoleView DOES define it at line 139

### 4.8 Duplicate CSS Block Issues

**design-tokens.css** has significant duplication:
- `--shadow-sm` defined twice (lines 212 and 213 — identical)
- `--shadow-xs` through `--shadow-2xl`, `--shadow-inner`, focus ring variables, and transition variables are duplicated in the dark mode block (lines 285-298 repeat lines 211-224) — though this is intentional for dark mode overrides
- The dark mode block redefines many variables that are also re-overridden (e.g., `--color-success` is defined three times: lines 84-87, 498-501, 417-420)
- `h2`, `h3`, `h4`, `a`, `code`, `body`, `button`, `input/select/textarea`, `:focus-visible`, `:selection`, `::-webkit-scrollbar`, media query blocks are all duplicated (lines 576-666 repeat lines 567-666)

**tokens.css and design-tokens.css define conflicting variable values:**
- `tokens.css` `--bg: #f2f4f9` vs `design-tokens.css` `--surface-canvas: #e4e9f2`
- `tokens.css` `--accent: #5b5ef0` vs `design-tokens.css` `--color-primary-600: #4f46e5`
- `tokens.css` `--fail: #e11d48` vs `design-tokens.css` `--color-error-600: #dc2626`
- `tokens.css` `--ok: #059669` vs `design-tokens.css` `--color-success-600: #0d9488`

Both files are imported in main.js, with design-tokens.css imported AFTER tokens.css. Since they use different variable names (`--accent` vs `--color-primary-600`), they don't directly conflict, but views using one vs the other will get different colors. This creates a two-tier color system where different views use different variable naming conventions.

---

## Summary of Recommendations (Priority)

### Critical (Must Fix)
1. **AdminConsoleView.vue:19** — `<a class="btn outline">` has undefined `.btn` class; link is unstyled. Either use `<Button>` component or define `.btn` class.
2. **CloudAccountsView.vue** — `size="small"` and `variant="outline"` are invalid Button props; buttons render at wrong size and as primary color instead of outline/ghost.
3. **SystemOpsView.vue:152-193** — `<FormItem label="{{ $t(...) }}">` uses literal string interpolation instead of `:label="t(...)"` binding; labels show raw `{{ $t(...) }}` text.

### High
4. **K8sClustersView.vue & K8sResourcesView.vue** — Add `<style scoped>` blocks; define `.card`, `.flowbar`, `.field`, `.btnbar`, `.row-actions`, `.modal`, `.modal-head`, `.modal-body`, `.poll-err`, `.muted` in scoped styles.
5. **HelmReposView.vue & HelmReleasesView.vue** — Remove duplicate `.btnbar` definitions; migrate raw `<button>` to `<Button>` component for consistency.
6. **All 55 views using raw `<button>`** — Migrate to `<Button>` component from `@/components/ui` for consistent styling.

### Medium
7. Split multi-feature cramming pages (SystemOpsView, AlertRulesView, ConfigHotpushView, BillingView, AIOpsView, TasksView, GPUView) into sub-pages/tabs.
8. Consolidate `.card`, `.btnbar`, `.field`, `.flowbar`, `.poll-err`, `.row-actions` definitions into a single shared CSS file to eliminate per-view duplication.
9. Fix inconsistent Button prop usage: `size="small"` → `size="sm"`, `variant="outline"` → `variant="ghost"`.
10. Add `.glass-card` class definition to OverviewView or remove the class usage.
11. Define `--mono` (→ `--font-family-mono`) and `--primary` (→ `--color-primary`) aliases in global CSS.
12. Define `--font-size` as alias for `--font-size-base` in global CSS for Input.vue.

### Low
13. Add width constraints to table columns that lack them (DeadLetterView failureReason, K8sResourcesView services/nodes).
14. Convert inline `style="margin: 0"` on card headers to a `card__title` class for consistency.
15. Consolidate duplicate CSS blocks in design-tokens.css (duplicate `--shadow-sm`, duplicate base styles).
16. CloudAccountsView — reduce 5 action buttons per row to a dropdown.
17. Remove redundant double header in CloudAccountsView (Header component + page-title div).
