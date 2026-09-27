package main

import "strings"

// ensureParseTime 保证 go-sql-driver 的 DSN 自带 parseTime=true。
//
// 背景（2026-09-25 线上缺陷的回归守护，原实现随死代码 mysql.go 一并移除后，
// 按原逻辑迁移到真实调用点）：生产清单（compose/Helm）给出的 DSN 已自带
// "?parseTime=true"，但自备 DSN（values/环境变量覆盖）不保证带。缺 parseTime 时
// time.Time 列扫描直接报错，sql backend 起不来；历史上该函数不幂等还会拼出
// 非法 DSN——两种形态都表现为"sql.Open 失败后服务静默退回内存存储"
// （部署成功、重启即丢数据）。幂等性由 dsn_test.go 守护。
func ensureParseTime(dsn string) string {
	if strings.Contains(dsn, "parseTime=") {
		return dsn
	}
	if strings.Contains(dsn, "?") {
		return dsn + "&parseTime=true"
	}
	return dsn + "?parseTime=true"
}
