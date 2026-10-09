// template_shim.go 模板格式类型的稳定门面（TD-87 批 1）。
//
// 类型定义、预置数据与校验/渲染函数已下沉 internal/controlplane/presets/；本文件以
// **类型别名**把五个名字保留在父包：父包内数十处 handler/测试的引用零改动
// （别名=同一类型——结构体字面量、字段访问、与 store 持久化模型之间的显式转换全部照旧）。
package controlplane

import "github.com/Levango7/OpsMesh/internal/controlplane/presets"

type (
	OSTemplate         = presets.OSTemplate
	OSParam            = presets.OSParam
	MiddlewareTemplate = presets.MiddlewareTemplate
	MiddlewareParam    = presets.MiddlewareParam
	MiddlewareScript   = presets.MiddlewareScript
)
