package sqlstore

// user_lookup_err_test.go — §3.6/§.7 的 SQL 侧守卫（2026-10-14）：GetUser*Err
// 变体必须把「查无此人」与「读失败」分成两种返回。
//
// 为什么：scanUser 把 sql.ErrNoRows 与真错误压成同一个 nil ⇒ 控制面 login 把
// 后端故障当成"invalid username or password"、refresh 当成"user not active"而清
// Cookie（演练两个人设独立复现：一次 DB 抖动 = 全体用户被击退 + 活跃用户被锁
// 15 分钟）。变体契约：(nil, nil)=查无、(nil, err)=读失败、(u, nil)=命中。

import (
	"database/sql"
	"errors"
	"testing"
)

// TestGetUserErr_BadDBReturnsError 读失败必须返回 error（而不是 nil）——
// 控制面据此返回 503 分流；若变回 nil，login 会把故障当查无（§3.6 原状回归）。
func TestGetUserErr_BadDBReturnsError(t *testing.T) {
	s := newSQLStoreWithBadDB()
	u, err := s.GetUserErr("x")
	if err == nil {
		t.Fatal("库不可达时应返回 error（不是 nil）——否则调用方无法区分查无与故障")
	}
	if u != nil {
		t.Errorf("读失败不应返回用户，实得 %+v", u)
	}
	if errors.Is(err, sql.ErrNoRows) {
		t.Error("连接失败不应被映射成 ErrNoRows（那正是'查无'的信号）")
	}
}

// TestGetUserByUsernameErr_AndID_ShareSemantics 两个导出变体语义一致
// （username/id 只是 WHERE 参数不同，契约必须同形）。
func TestGetUserByUsernameErr_AndID_ShareSemantics(t *testing.T) {
	s := newSQLStoreWithBadDB()
	for name, fn := range map[string]func(string) (*User, error){
		"GetUserByUsernameErr": s.GetUserByUsernameErr,
		"GetUserErr":           s.GetUserErr,
	} {
		u, err := fn("whatever")
		if err == nil || u != nil {
			t.Errorf("%s 库不可达时应 (nil, error)，实得 (%+v, %v)", name, u, err)
		}
	}
}
