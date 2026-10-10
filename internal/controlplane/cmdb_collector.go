// cmdb_collector.go CMDB 采集的 Server 端接线 + 采集器门面（TD-87 批 2 第三批：
// 采集器实现（类型/构造/采集循环）已下沉 internal/controlplane/cmdbcollector）。
package controlplane

import (
	"net/http"
	"time"

	"github.com/Levango7/OpsMesh/internal/cmdb"
	"github.com/Levango7/OpsMesh/internal/controlplane/cmdbcollector"
	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
	"github.com/Levango7/OpsMesh/internal/store"
)

// CMDBCollector 采集器别名 + 构造薄包装（实现见 cmdbcollector 包）。
// server.go 的字段声明与构造调用因此零改动。
type CMDBCollector = cmdbcollector.Collector

func NewCMDBCollector(st store.Store, ci cmdb.CiStore, interval time.Duration, tenantID string) *CMDBCollector {
	return cmdbcollector.New(st, ci, interval, tenantID)
}

// handleCMDBCollect 处理 POST /api/v1/cmdb/collect — 手动触发全量采集。
//
// 不经过 leader 校验（手动触发允许任意副本执行，适合运维干预场景）。
// 返回 {"collected": N, "failed": M}。
//
// 鉴权：需 cmdb:write 权限（requireProd 校验）。
func (s *Server) handleCMDBCollect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed, use POST"})
		return
	}
	if _, ok := s.requireProd(w, r, "cmdb:write"); !ok {
		return
	}
	if s.cmdbCollector == nil {
		paginate.WriteJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "cmdb collector not initialized"})
		return
	}
	collected, failed, err := s.cmdbCollector.CollectAll()
	if err != nil {
		writeInternalError(r.Context(), w, "cmdbCollector.collectAll", err)
		return
	}
	paginate.WriteJSON(w, http.StatusOK, map[string]int{
		"collected": collected,
		"failed":    failed,
	})
}
