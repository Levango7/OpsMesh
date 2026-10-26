// script_body.go — 任务「多行脚本体」通道的执行侧实现。
//
// 为什么需要这条通道：内联命令通道（proto.Task.Command）对换行是**硬拦截**
// （checkShellMetachars："ls\nrm -rf /" 类拼接注入防护），而预置 OS 优化模板
// 与中间件部署模板天然是多行 bash（heredoc、函数定义、for 循环）。
// 两者直接冲突 ⇒ 「OS 优化」等模板 100% 失败，且报的是误导性的
// "shell command contains dangerous metacharacters (newline)"。
//
// 本文件实现「整段脚本」的落盘-执行-清理闭环：
//
//	脚本体 → <tmpdir>/opsmesh-script-<taskID>.sh（0600）→ sh <file> → 删除
//
// 安全边界（与内联命令通道互不越界）：
//   - 内联 Command 字段仍按原样校验（元字符拦截 + 命令白名单），单行路径行为不变；
//   - 脚本体**不参与**内联换行禁令——它是一整段脚本，不是可被拼接的内联命令；
//   - 脚本体只能由控制面模板路径写入（OS 模板 execute / 中间件 deploy、uninstall），
//     普通任务创建接口的请求体不含该字段，外部无法注入；
//   - 模板参数值在替换进脚本体之前统一过 presets.ValidateShellSafeValues。
package agent

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/Levango7/OpsMesh/internal/proto"
)

// scriptFilePrefix 临时脚本文件名前缀（<tmpdir>/opsmesh-script-<taskID>.sh）。
const scriptFilePrefix = "opsmesh-script-"

// scriptFileSuffix 临时脚本文件后缀。
const scriptFileSuffix = ".sh"

// scriptFilePerm 临时脚本文件权限：0600（仅属主可读写）。
//
// 为什么必须收紧：临时目录在 Linux 上通常是 /tmp（全局可写 + sticky），
// 默认 umask 022 下建出来的文件是 0644 —— 同机其他用户可读脚本内容，
// 而模板脚本里可能带中间件口令、连接串等占位值。
const scriptFilePerm os.FileMode = 0o600

// defaultScriptCommand 是脚本体任务的默认调用形态（同时是白名单可见的声明）。
//
// 它是**标记**而非执行向量：agent 实际执行的是 `sh <临时脚本路径>`，
// 但 Command 字段仍要过一遍内联元字符拦截 + 命令白名单——白名单里没有 sh 的节点
// 不允许执行脚本类任务（与「该节点不允许执行任意命令」是同一条边界）。
const defaultScriptCommand = "sh " + scriptPathPlaceholder

// scriptPathPlaceholder 脚本文件路径占位符（与 internal/proto.ScriptPathPlaceholder 同值）。
//
// 这里独立定义常量而不在 agent 包里 import proto 的常量，是为了避免两处字面量
// 漂移；TestScriptPathPlaceholderMatchesProto 断言两者一致。
const scriptPathPlaceholder = "{script}"

// executeScriptBody 执行任务脚本体：落临时文件 → `sh <file>` → 删除临时文件。
//
// 清理语义：cleanup 由 defer 保证在**所有**退出路径（成功、失败、超时、取消）上执行，
// 磁盘上不残留脚本副本。
func (a *Agent) executeScriptBody(ctx context.Context, t proto.Task, stdout, stderr io.Writer) error {
	path, cleanup, err := writeScriptFile(t.TaskID, t.ScriptBody)
	if err != nil {
		return fmt.Errorf("write script file: %w", err)
	}
	defer cleanup()
	return a.executeScriptFile(ctx, path, stdout, stderr)
}

// executeScriptFile 以 `sh <file>` 执行临时脚本文件。
//
// 为什么直接 exec 解释器而不是把路径拼进 shell 命令串：
// 路径可能含空格（Windows 的 C:\Users\<带空格用户名>\AppData\Local\Temp），
// 拼进 `sh -c "..."` / `cmd /C "..."` 后要处理两套完全不同的引号规则，
// 拼错就是「路径被截断 → 执行了别的文件」。这里把路径作为独立 argv 元素传给
// 解释器，从根上绕开引号问题。
func (a *Agent) executeScriptFile(ctx context.Context, path string, stdout, stderr io.Writer) error {
	cmd := scriptFileCommand(path)
	return a.runProcess(ctx, cmd, stdout, stderr)
}

// scriptFileCommand 构造执行临时脚本的子进程。
//
// Unix：sh <file>（POSIX sh；预置模板只用 POSIX 语法，shebang 行按注释处理）。
// Windows：优先 sh（Git Bash / MSYS 环境有），没有则退到 cmd /C <file>
//   ——Windows 上的 agent 本就不是预置 bash 模板的目标平台，退化路径只为
//   「不因缺少 sh 而整条任务失败」，脚本语义由 cmd 解释（复杂 bash 语法会失败，
//   但会如实报错，不会静默跳过）。
func scriptFileCommand(path string) *exec.Cmd {
	if runtime.GOOS == "windows" {
		if _, err := exec.LookPath("sh"); err != nil {
			return exec.Command("cmd", "/C", path)
		}
	}
	return exec.Command("sh", path)
}

// writeScriptFile 把脚本体落成临时脚本文件，返回文件路径与清理函数。
//
// 命名：<tmpdir>/opsmesh-script-<taskID>.sh——带任务 ID 便于排障（任务详情里
// 能对上号）。用 O_EXCL 创建：既不会覆盖同名既有文件，也挡得住「预先放一个
// 符号链接骗我们往任意路径写」的经典攻击。名字已被占用时（同名任务重试、
// 并发执行）退到随机名（os.CreateTemp，同样 0600），不覆盖、不复用。
//
// 清理：返回的 cleanup 必须在所有退出路径上执行（调用方 defer）。
func writeScriptFile(taskID, body string) (string, func(), error) {
	dir := os.TempDir()
	path := filepath.Join(dir, scriptFilePrefix+sanitizeFileNameComponent(taskID)+scriptFileSuffix)
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, scriptFilePerm)
	if err != nil {
		// 名字已存在（或目录不可写）：退到随机名，避免覆盖他人文件。
		f, err = os.CreateTemp(dir, scriptFilePrefix+"*"+scriptFileSuffix)
		if err != nil {
			return "", nil, err
		}
		path = f.Name()
	}
	cleanup := func() { _ = os.Remove(path) }
	if _, err := f.WriteString(body); err != nil {
		_ = f.Close()
		cleanup()
		return "", nil, err
	}
	if err := f.Close(); err != nil {
		cleanup()
		return "", nil, err
	}
	return path, cleanup, nil
}

// sanitizeFileNameComponent 把任务 ID 收敛成安全的文件名片段。
//
// 任务 ID 来自控制面（形如 task-1753...-1），但仍按不可信输入处理：
// 只保留字母/数字/点/下划线/连字符，其余一律替换为下划线，并截断长度，
// 防目录穿越（../）与超长文件名。
func sanitizeFileNameComponent(s string) string {
	const maxLen = 64
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '.', r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
		if b.Len() >= maxLen {
			break
		}
	}
	out := b.String()
	if out == "" {
		return "task"
	}
	return out
}
