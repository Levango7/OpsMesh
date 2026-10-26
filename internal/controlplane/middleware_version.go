// Package controlplane: middleware_version.go 解析中间件模板的版本选择与镜像占位符。
//
// 预置中间件模板（presets.MiddlewareTemplates）的 docker 部署脚本统一以 {image} 占位镜像引用，
// 实际镜像由所选版本的 DockerImage 决定；未显式选择版本时回落到模板默认版本，
// 保证改造前后的部署行为一致（默认版本镜像即原脚本中硬编码的镜像）。
package controlplane

import (
	"strings"

	"github.com/Levango7/OpsMesh/internal/controlplane/presets"
)

// resolveMiddlewareVersion 按版本号解析模板的目标版本。
// version 为空或未匹配任何版本时回落到默认版本；模板未声明版本目录时返回 nil。
func resolveMiddlewareVersion(tpl *MiddlewareTemplate, version string) *presets.MiddlewareVersion {
	if tpl == nil || len(tpl.Versions) == 0 {
		return nil
	}
	if version != "" {
		for i := range tpl.Versions {
			if tpl.Versions[i].Version == version {
				return &tpl.Versions[i]
			}
		}
	}
	for i := range tpl.Versions {
		if tpl.Versions[i].Default {
			return &tpl.Versions[i]
		}
	}
	return &tpl.Versions[0]
}

// resolveMiddlewareScript 解析指定部署方式下最终使用的脚本：
//   - 版本自带同 deployType 的脚本时，以版本脚本为准；
//   - docker 脚本中的 {image} 占位符替换为所选版本的镜像。
func resolveMiddlewareScript(tpl *MiddlewareTemplate, deployType string, version string) (MiddlewareScript, bool) {
	if tpl == nil {
		return MiddlewareScript{}, false
	}
	script, ok := tpl.Scripts[deployType]
	if !ok {
		return MiddlewareScript{}, false
	}
	v := resolveMiddlewareVersion(tpl, version)
	if v == nil {
		return script, true
	}
	if vs, ok := v.Scripts[deployType]; ok {
		script = vs
	}
	if v.DockerImage != "" {
		script.Deploy = strings.ReplaceAll(script.Deploy, "{image}", v.DockerImage)
	}
	return script, true
}

// resolveMiddlewareParams 合并模板参数与版本特有参数：版本参数同名时覆盖模板参数，
// 不同名时追加到模板参数之后。
func resolveMiddlewareParams(tpl *MiddlewareTemplate, version string) []MiddlewareParam {
	if tpl == nil {
		return nil
	}
	params := append([]MiddlewareParam(nil), tpl.Params...)
	v := resolveMiddlewareVersion(tpl, version)
	if v == nil || len(v.Params) == 0 {
		return params
	}
	for _, vp := range v.Params {
		replaced := false
		for i := range params {
			if params[i].Name == vp.Name {
				params[i] = vp
				replaced = true
				break
			}
		}
		if !replaced {
			params = append(params, vp)
		}
	}
	return params
}

// selectedMiddlewareVersion 从部署请求中提取所选版本号：
// 优先取请求体的 version 字段，其次取 params 中的 version。
func selectedMiddlewareVersion(bodyVersion string, params map[string]string) string {
	if bodyVersion != "" {
		return bodyVersion
	}
	return params["version"]
}
