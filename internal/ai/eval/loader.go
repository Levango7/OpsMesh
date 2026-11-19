package eval

import (
	"embed"
	"encoding/json"
	"fmt"
)

// casesFS 嵌入评估集样本，随二进制分发，离线可读。
//
//go:embed testdata/*.json
var casesFS embed.FS

// LoadCases 从嵌入的 testdata 读取并解析评估集。
// name 为文件名（如 "eval-cases.json"）；空集合或非法 JSON 返回错误。
func LoadCases(name string) (CaseSet, error) {
	if name == "" {
		name = "eval-cases.json"
	}
	data, err := casesFS.ReadFile("testdata/" + name)
	if err != nil {
		return CaseSet{}, fmt.Errorf("eval: read %s: %w", name, err)
	}
	var set CaseSet
	if err := json.Unmarshal(data, &set); err != nil {
		return CaseSet{}, fmt.Errorf("eval: parse %s: %w", name, err)
	}
	if len(set.Cases) == 0 {
		return CaseSet{}, fmt.Errorf("eval: %s has no cases", name)
	}
	if set.Version == "" {
		set.Version = "unversioned"
	}
	return set, nil
}
