package gpu

import "testing"

// npu-smi 的 `Key: value` 形态（ascend.go 的解析器就按这个形状写的）。
const ascendSample = `NPU ID: 0
Name: Ascend 910B3
HBM Capacity: 32768MB
Temperature: 35C
Power: 72.5W
NPU ID: 1
Name: Ascend 910B3
HBM Capacity: 32768 MB
Temperature: 41 C
Power: 68W
`

func TestParseAscendOutput_ParsesTwoCards(t *testing.T) {
	gpus := parseAscendOutput(ascendSample)
	if len(gpus) != 2 {
		t.Fatalf("应解析出 2 张卡，实得 %d：%+v", len(gpus), gpus)
	}
	if gpus[0].ID != "0" || gpus[0].MemoryTotalMB != 32768 {
		t.Errorf("卡0 解析错：ID=%q Mem=%d", gpus[0].ID, gpus[0].MemoryTotalMB)
	}
	if gpus[0].Temperature != 35 || gpus[1].Temperature != 41 {
		t.Errorf("温度解析错：卡0=%v 卡1=%v", gpus[0].Temperature, gpus[1].Temperature)
	}
}

// 旧实现用 fmt.Sscanf("%d") 会把 "72.5W" 截成 72 —— 小数部分被静默丢掉。
func TestParseAscendOutput_KeepsFractionalPower(t *testing.T) {
	gpus := parseAscendOutput(ascendSample)
	if gpus[0].PowerDraw != 72.5 {
		t.Errorf("PowerDraw 应保留小数 72.5，实得 %v（被截断成整数就是旧缺陷）", gpus[0].PowerDraw)
	}
	if gpus[1].PowerDraw != 68 {
		t.Errorf("PowerDraw 整数形态应解成 68，实得 %v", gpus[1].PowerDraw)
	}
}

// 解析不了时保持零值，绝不写进一个错数字。
// 已知缺口（不在本函数射程内）：GPUInfo 没有"未知"表示，所以零值与真实 0W/0°C 不可区分。
func TestParseAscendOutput_UnparsableMetricStaysZero(t *testing.T) {
	gpus := parseAscendOutput("NPU ID: 0\nPower: N/A W\nTemperature: -- C\n")
	if len(gpus) != 1 {
		t.Fatalf("应仍识别出 1 张卡，实得 %d", len(gpus))
	}
	if gpus[0].PowerDraw != 0 || gpus[0].Temperature != 0 {
		t.Errorf("不可解析的指标应为零值，实得 Power=%v Temp=%v", gpus[0].PowerDraw, gpus[0].Temperature)
	}
}
