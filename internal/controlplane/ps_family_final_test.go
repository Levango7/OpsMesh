package controlplane

import (
	"net/http"
	"os"
	"strings"
	"testing"
)

// TestPSFamilyFinalSweep 覆盖批次 6（PowerShell 家族最后一处收口）的全部 13 处：
//
// 为什么这批不是纯风格改动（pkg/shellquote/powershell.go 头注记了同一形状的实测）：
// Windows agent 用 `cmd /C` 执行脚本，Go 的 exec 参数转义会改写内层引号 ⇒
// `powershell -Command "..."` 形式会让脚本被回显、根本不执行；同一路径下 cmd 认不出
// Unix 的 `2>/dev/null`（整行报废），也认不出 Unix 的 `| head -40`。
//
// 判据（逐条逐字）：
//
//	① wire 形态必须走 `-EncodedCommand`，且不得残留 `powershell -Command `；
//	② 不得残留 `2>/dev/null`，必须有 cmd 尾巴 `2>nul`；
//	③ base64 解码回来的 PS 载荷必须与预期脚本**逐字相等**（编码错一个字就判红）。
func TestPSFamilyFinalSweep(t *testing.T) {
	cases := []struct {
		name    string
		handler func(*Server) http.HandlerFunc
		body    map[string]interface{}
		want    string
		tail    string
	}{
		{
			name:    "firewall list",
			handler: func(s *Server) http.HandlerFunc { return s.handleWindowsFirewall },
			body:    map[string]interface{}{"deviceID": "ps6-fw-list", "action": "list"},
			want:    `Get-NetFirewallRule | Select-Object DisplayName, Direction, Action, Enabled | Format-Table -AutoSize | Select-Object -First 40`,
			tail:    " 2>nul",
		},
		{
			name:    "firewall status",
			handler: func(s *Server) http.HandlerFunc { return s.handleWindowsFirewall },
			body:    map[string]interface{}{"deviceID": "ps6-fw-status", "action": "status"},
			want:    `Get-NetFirewallProfile | Select-Object Name, Enabled`,
			tail:    " 2>nul",
		},
		{
			name:    "firewall enable",
			handler: func(s *Server) http.HandlerFunc { return s.handleWindowsFirewall },
			body:    map[string]interface{}{"deviceID": "ps6-fw-enable", "action": "enable"},
			want:    `Set-NetFirewallProfile -Profile Domain,Public,Private -Enabled True`,
			tail:    " 2>nul && echo Firewall enabled",
		},
		{
			name:    "firewall disable",
			handler: func(s *Server) http.HandlerFunc { return s.handleWindowsFirewall },
			body:    map[string]interface{}{"deviceID": "ps6-fw-disable", "action": "disable"},
			want:    `Set-NetFirewallProfile -Profile Domain,Public,Private -Enabled False`,
			tail:    " 2>nul && echo Firewall disabled",
		},
		{
			name:    "user list",
			handler: func(s *Server) http.HandlerFunc { return s.handleWindowsUser },
			body:    map[string]interface{}{"deviceID": "ps6-user-list", "action": "list"},
			want:    `Get-LocalUser | Select-Object Name, Enabled, LastLogon | Format-Table -AutoSize`,
			tail:    " 2>nul",
		},
		{
			name:    "perf cpu",
			handler: func(s *Server) http.HandlerFunc { return s.handleWindowsPerf },
			body:    map[string]interface{}{"deviceID": "ps6-perf-cpu", "action": "cpu"},
			want:    `Get-Counter '\Processor(_Total)\% Processor Time' -SampleInterval 1 -MaxSamples 3 | Select-Object -ExpandProperty CounterSamples | Select-Object CookedValue`,
			tail:    " 2>nul",
		},
		{
			name:    "perf memory",
			handler: func(s *Server) http.HandlerFunc { return s.handleWindowsPerf },
			body:    map[string]interface{}{"deviceID": "ps6-perf-mem", "action": "memory"},
			want:    `Get-CimInstance Win32_OperatingSystem | Select-Object TotalVisibleMemorySize, FreePhysicalMemory, TotalVirtualMemorySize, FreeVirtualMemory`,
			tail:    " 2>nul",
		},
		{
			name:    "perf disk",
			handler: func(s *Server) http.HandlerFunc { return s.handleWindowsPerf },
			body:    map[string]interface{}{"deviceID": "ps6-perf-disk", "action": "disk"},
			want:    `Get-PSDrive -PSProvider FileSystem | Select-Object Name, Used, Free, @{N='Total';E={$_.Used+$_.Free}}`,
			tail:    " 2>nul",
		},
		{
			name:    "perf network",
			handler: func(s *Server) http.HandlerFunc { return s.handleWindowsPerf },
			body:    map[string]interface{}{"deviceID": "ps6-perf-net", "action": "network"},
			want:    `Get-NetAdapterStatistics | Select-Object Name, ReceivedBytes, SentBytes`,
			tail:    " 2>nul",
		},
		{
			name:    "perf processes",
			handler: func(s *Server) http.HandlerFunc { return s.handleWindowsPerf },
			body:    map[string]interface{}{"deviceID": "ps6-perf-proc", "action": "processes"},
			want:    `Get-Process | Sort-Object CPU -Descending | Select-Object -First 20 Name, CPU, WorkingSet, Id | Format-Table -AutoSize`,
			tail:    " 2>nul",
		},
		{
			name:    "service list",
			handler: func(s *Server) http.HandlerFunc { return s.handleWindowsService },
			body:    map[string]interface{}{"deviceID": "ps6-svc-list", "action": "list"},
			want:    `Get-Service | Select-Object Name, Status, StartType | Format-Table -AutoSize`,
			tail:    " 2>nul",
		},
		{
			name:    "service status",
			handler: func(s *Server) http.HandlerFunc { return s.handleWindowsService },
			body:    map[string]interface{}{"deviceID": "ps6-svc-status", "action": "status"},
			want:    `Get-Service | Select-Object Name, Status | Format-Table -AutoSize`,
			tail:    " 2>nul",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := newShellQuoteServer()
			rec := sqPost(t, s, c.handler(s), c.body)
			if rec.Code != http.StatusAccepted {
				t.Fatalf("应受理：%d %s", rec.Code, rec.Body.String())
			}
			dev := c.body["deviceID"].(string)
			sc := sqDispatchScript(t, s, dev)
			ps := assertPSWrapped(t, sc, "")
			if ps != c.want {
				t.Errorf("PS 载荷不等：\n got=%s\nwant=%s", ps, c.want)
			}
			if !strings.HasSuffix(sc, c.tail) {
				t.Errorf("尾巴不等：\n got=%q\nwant=%q", sc, c.tail)
			}
		})
	}
}

// TestPSFamilyFinalSweep_TcpingWindows 覆盖本批唯一带插值的一处：
// tcping on windows 的 target 走 QuotePowerShellSingle、端口走 strconv（纯数字）。
func TestPSFamilyFinalSweep_TcpingWindows(t *testing.T) {
	cmd, err := buildDiagnoseCommand("tcping", "10.0.0.1", diagnoseOptions{Port: 80}, "windows")
	if err != nil {
		t.Fatalf("tcping windows: %v", err)
	}
	if strings.Contains(cmd, "powershell -Command ") {
		t.Fatalf("仍是旧式 -Command 形态：%s", cmd)
	}
	ps := decodePSCommand(t, cmd)
	want := `Test-NetConnection -ComputerName '10.0.0.1' -Port 80`
	if ps != want {
		t.Fatalf("PS 载荷不等：\n got=%s\nwant=%s", ps, want)
	}
}

// TestPSFamilyFinalSweep_NoLegacyCommandForm 兜底：controlplane 包的生产文件里
// 不得再出现 `powershell -Command `（旧式形态在 cmd /C 路径上会让脚本被回显而不执行）。
// 仓级扫描由 scripts/shell-injection-audit.py 负责；此处守本批改动的三个文件。
func TestPSFamilyFinalSweep_NoLegacyCommandForm(t *testing.T) {
	for _, f := range []string{"git_unix_ops.go", "os_ext_ops.go", "server_network.go"} {
		body, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("读取 %s 失败：%v", f, err)
		}
		if strings.Contains(string(body), "powershell -Command ") {
			t.Errorf("%s 仍残留旧式 `powershell -Command `", f)
		}
	}
}
