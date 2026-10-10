// no_dup_impl_test.go 「机密静态加密不得出现第二份实现」的静态守卫（TD-88 前置，规格 §5-2）。
//
// # 为什么需要
//
// 同一职责曾有三份同形态实现并存（config-svc / controlplane k8s / TD-88 将新增），
// 而缺陷只在其中一份里发生过（config-svc 的 MySQL 后端曾把机密原样落库）——
// 教训写在注释里没用，本守卫把它变成机器判据：**全仓 `aes.NewCipher(`/`cipher.NewGCM(`
// 的实现只允许出现在 pkg/secretcrypto**，多一处即判红并点名文件与行号。
//
// 扫描面：全仓 *.go（排除 `_test.go` —— 测试里可以内嵌「旧实现」做交叉验证，
// 那正是迁移期要的证据；也排除本包自身）。
//
// # 防塌缩
//
// 与门禁第 21/24 节同哲学：断言带下限（扫描到的 .go 文件数 ≥ floor），
// 扫描面失效（路径算错/被跳过）时判红而不是安静变绿。
package secretcrypto

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// scannedFilesFloor 扫描文件数下限，只许上调（当前全仓非测试 .go 实测数百）。
const scannedFilesFloor = 200

// selfImplFloor 本包自身必须仍持有实现（防止「把实现也删了」的假绿）。
const selfImplFloor = 2 // aes.NewCipher + cipher.NewGCM 各至少一次

func TestNoDuplicateCryptoImplementation(t *testing.T) {
	root := cryptoRepoRoot(t)
	patterns := []string{"aes.NewCipher(", "cipher.NewGCM("}
	var offenders []string
	scanned := 0
	selfHits := 0

	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, werr error) error {
		if werr != nil {
			return nil
		}
		name := d.Name()
		if d.IsDir() {
			switch name {
			case ".git", "node_modules", "vendor", "testdata", ".tmp", "tmp":
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			return nil
		}
		scanned++
		data, rerr := os.ReadFile(p)
		if rerr != nil {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		isSelf := strings.HasPrefix(filepath.ToSlash(rel), "pkg/secretcrypto/")
		for i, line := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "//") {
				continue // 注释里可以「提到」这两个 API（注释不是实现），跳过以免假阳性
			}
			for _, pat := range patterns {
				if !strings.Contains(line, pat) {
					continue
				}
				if isSelf {
					selfHits++
					continue
				}
				offenders = append(offenders, filepath.ToSlash(rel)+":"+itoa(i+1)+"  "+strings.TrimSpace(line))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("扫描全仓失败: %v", err)
	}

	if offenders != nil {
		t.Errorf("机密静态加密出现了第二份实现（应为 0 处，实际 %d 处）——"+
			"请改用 pkg/secretcrypto.Encrypt/Decrypt（格式必须是 enc:v1: + base64(nonce||GCM)）：\n  %s",
			len(offenders), strings.Join(offenders, "\n  "))
	}
	if scanned < scannedFilesFloor {
		t.Fatalf("扫描面只覆盖 %d 个 .go 文件（< 下限 %d）——守卫失效（路径算错/被跳过），不是「全仓合规」",
			scanned, scannedFilesFloor)
	}
	if selfHits < selfImplFloor {
		t.Fatalf("pkg/secretcrypto 自身只扫到 %d 处实现（< 下限 %d）——实现被删空或移走，守卫在守一个空壳",
			selfHits, selfImplFloor)
	}
	t.Logf("全仓 %d 个非测试 .go 文件已扫描：实现仅存在于 pkg/secretcrypto（%d 处）", scanned, selfHits)
}

// cryptoRepoRoot 从测试工作目录（包目录）向上找含 go.work 的仓库根。
func cryptoRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	for i := 0; i < 10; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.work")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	t.Fatal("向上找不到含 go.work 的仓库根（守卫无法确定扫描面）")
	return ""
}

// itoa 极简整数转字符串（避免为一个调用引入 strconv 之外的可读性问题）。
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
