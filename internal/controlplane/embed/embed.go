package embed

import "embed"

// WebFS 内嵌前端静态资源（前端独立化：HTML 从 Go 字符串抽离为独立文件，
// 为后续 Vue3 演进留门，MVP 继续 vanilla）。go:embed 在编译期打包，无运行时 IO 依赖。
//
//go:embed web
var WebFS embed.FS

// EnterpriseFS 内嵌企业版前端（Vue3 + Vite）构建产物，由控制面以 /enterprise/ 前缀分发（P0-3）。
//
// 为什么要单独一个 embed.FS：go:embed 不能跨目录，企业版产物必须位于本包目录内。
// 目录内只有 placeholder.html 入库（构建产物经 .gitignore 排除），故源码构建（未装 Node）
// 得到的是占位页，`deploy/docker/scripts/build-enterprise-web.sh` 或镜像构建阶段
// 会把 web/enterprise/dist/ 覆盖进来——此时即内置真实企业版前端。
// 是否内置由 enterprise_ui.go 的 bundleAvailable() 判定（占位页不含标记即视为已内置）。
//
//go:embed enterprise
var EnterpriseFS embed.FS
