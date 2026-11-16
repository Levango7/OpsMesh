// envelope_v2.go — 列表响应 v2 统一信封（契约收敛 BB-01/BB-02/BB-04）。
//
// 背景：仓内列表端点历史上并存 4 种形状（裸数组 / 领域名键 / {items} / {data,total,...}），
// 且五套分页词汇（page/pageSize、offset/limit、limit(100/1000)、top…）——外部脚本按文档传
// pageSize 会被静默忽略，拿到"看似成功但口径不同"的结果（qa-sweep BB-01/02/04）。
//
// 方案（owner 裁决 B）：冻结唯一 v2 信封 = {data,total,page,pageSize,hasMore}
// （与现存 /tasks /alerts 的 PaginateResult 一致，规则相对统一、字段互相能对上）；
// 存量端点保持旧形状，客户端显式传 ?envelope=v2 才切到新信封——向后兼容、可灰度。
//
// 本文件只提供 helper，不主动改任何 handler 的默认行为。
package paginate

import (
	"net/http"
	"net/url"
	"reflect"
)

// WantEnvelopeV2 判断请求是否显式要求 v2 列表信封（?envelope=v2）。
// 任何其它值（含缺省、envelope=v1）都保持旧行为——opt-in 而非 opt-out。
func WantEnvelopeV2(q url.Values) bool {
	return q.Get("envelope") == "v2"
}

// WriteListV2 以 v2 信封写出一页列表：{data,total,page,pageSize,hasMore}。
// allData 必须是可切片的集合（[]T）；调用方传全量，本函数按 page/pageSize 切片并回显实际生效参数。
// page<=0 视为不分页（写全量 + page=1/pageSize=len，hasMore=false），与旧"不传 page 返回全量"兼容。
func WriteListV2(w http.ResponseWriter, status int, allData interface{}, page, pageSize int) {
	total := sliceLen(allData)
	if page <= 0 {
		WriteJSON(w, status, PaginateResult{
			Data: allData, Total: total, Page: 1, PageSize: total, HasMore: false,
		})
		return
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	if pageSize > 200 {
		pageSize = 200
	}
	start := (page - 1) * pageSize
	if start > total {
		start = total
	}
	end := start + pageSize
	if end > total {
		end = total
	}
	WriteJSON(w, status, PaginateResult{
		Data: sliceRange(allData, start, end), Total: total,
		Page: page, PageSize: pageSize, HasMore: end < total,
	})
}

// sliceLen/sliceRange 用反射支持任意 []T，避免每个端点写一遍切片样板。
func sliceLen(v interface{}) int {
	if v == nil {
		return 0
	}
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Slice && rv.Kind() != reflect.Array {
		return 0
	}
	return rv.Len()
}

func sliceRange(v interface{}, start, end int) interface{} {
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Slice && rv.Kind() != reflect.Array {
		return v
	}
	if start >= rv.Len() {
		return rv.Slice(0, 0).Interface()
	}
	if end > rv.Len() {
		end = rv.Len()
	}
	return rv.Slice(start, end).Interface()
}
