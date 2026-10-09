// types.go 模板「格式」的类型定义（TD-87 批 1：随预置数据从父包迁入）。
//
// 这三种类型是**控制面的运行时模板格式**（不是 store 的持久化模型——持久化侧是
// internal/store 的 OSTemplate/MiddlewareTemplate，两者在 seed 处显式转换）。
// 父包以类型别名回导（controlplane/template_shim.go），外部与既有引用零改动。
package presets

// OSTemplate 预置 OS 优化任务模板。
// Commands 为一段 shell 脚本（在目标 Linux 主机以 `sh -c` 执行）；
// 需要参数的模板在脚本内通过 $1/$2/... 引用（旧模式）或 {name}/{port}/... 占位符引用（新模式），
// execute 时由控制面注入位置参数或做占位符替换。
type OSTemplate struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Category    string    `json:"category"` // kernel/network/security/time/ssh/disk/system/user
	Description string    `json:"description"`
	Commands    string    `json:"commands"`         // shell 脚本（可用 #!/bin/bash 开头）
	Risk        string    `json:"risk"`             // low/medium/high
	Tags        []string  `json:"tags"`             // 标签
	OS          string    `json:"os"`               // 适用操作系统：centos/ubuntu/all
	Params      []OSParam `json:"params,omitempty"` // 参数定义（新模式占位符替换 + 验证）
}

// OSParam OS 优化模板参数定义。
type OSParam struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Default     string `json:"default"`
	Required    bool   `json:"required"`
	Type        string `json:"type"` // string/int
}

// MiddlewareTemplate 预置中间件部署模板。
// Scripts 按 deployType（"docker"/"systemd"）索引对应部署/验证/卸载脚本。
type MiddlewareTemplate struct {
	ID          string                      `json:"id"`
	Name        string                      `json:"name"`
	Category    string                      `json:"category"` // database/cache/message/web/search
	Version     string                      `json:"version"`
	Description string                      `json:"description"`
	DeployTypes []string                    `json:"deployTypes"` // ["docker","systemd"]
	Params      []MiddlewareParam           `json:"params"`
	Scripts     map[string]MiddlewareScript `json:"scripts"` // key: "docker"/"systemd"
	Risk        string                      `json:"risk"`    // low/medium/high
	Tags        []string                    `json:"tags"`
}

// MiddlewareParam 中间件部署参数定义。
type MiddlewareParam struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Default     string `json:"default"`
	Required    bool   `json:"required"`
	Type        string `json:"type"` // string/int/bool
}

// MiddlewareScript 部署脚本三元组：部署/验证/卸载。
// 脚本内可使用 {name}/{port}/{password}/... 等占位符，deploy 时由 params 替换。
type MiddlewareScript struct {
	Deploy    string `json:"deploy"`    // 部署命令
	Verify    string `json:"verify"`    // 验证/健康检查命令
	Uninstall string `json:"uninstall"` // 卸载命令
}
