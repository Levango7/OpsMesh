// Package auditsafe 审计/事件 Detail 的脱敏与规范化（TD-87 批 2 第三批：自 server_tasks.go 抽出）。
//
// 为什么单独成包：它此前定义在 **server_tasks.go**（任务域文件）里，却被 6+ 个 handler 文件
// 共 30 处调用——典型的「共享内核藏在业务文件里」；当新下沉的包（cmdbcollector）也需要它时，
// 藏在父包里就成了跨包障碍。抽出后父包留同名薄包装，30 处调用点零改动。
package auditsafe

import "strings"

// Detail 对写入审计/事件 Detail 的用户输入做脱敏与规范化：
//   - 移除换行符（\n→空格、\r→删除），防日志注入/解析错位；
//   - 截断超过 200 字符的内容，避免长命令撑爆日志、可能携带的敏感尾部外泄。
//
// 仅用于含用户原始输入（body.Command / body.Reason）的 Detail；固定字符串无需调用。
func Detail(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", "")
	if len(s) > 200 {
		s = s[:200] + "..."
	}
	return s
}
