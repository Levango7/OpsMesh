// errors.go 存储层共享的哨兵错误（TD-61）。
//
// 判据：同一错误被 memory 与 sql 两个后端的任一实现返回、任一调用方 errors.Is 比较，
// 就必须只有一份定义——否则两后端对"同一个错"给出不同值，调用方的 errors.Is 会漏判。
package model

import "errors"

// ErrRefreshTokenHashRequired 入参校验错误：TokenHash 为空时拒绝（主键不可空）。
//
// 原定义在 memory_refresh.go 而 sql_refresh.go 也在用（TD-61 上提中性层，单一来源）。
var ErrRefreshTokenHashRequired = errors.New("refresh token: tokenHash required")
