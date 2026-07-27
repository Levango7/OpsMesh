package controlplane

import "embed"

// webFS 内嵌前端静态资源（E2 前端独立化：HTML 从 Go 字符串抽离为独立文件，
// 为后续 Vue3 演进留门，MVP 继续 vanilla）。go:embed 在编译期打包，无运行时 IO 依赖。
//
//go:embed web
var webFS embed.FS
