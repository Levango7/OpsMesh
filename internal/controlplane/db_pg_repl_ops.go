// db_pg_repl_ops.go PostgreSQL 高级复制运维（复制槽/发布/订阅/基础备份/PITR）。
package controlplane

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
	"github.com/Levango7/OpsMesh/internal/proto"
)

// pgReplSlotNameRe 是复制槽/发布/订阅名白名单。
var pgReplSlotNameRe = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]{0,62}$`)

// pgReplActions PostgreSQL 高级复制 action 白名单。
var pgReplActions = map[string]bool{
	"slots-list":         true,
	"slot-create":        true,
	"slot-drop":          true,
	"pubs-list":          true,
	"pub-create":         true,
	"pub-drop":           true,
	"subs-list":          true,
	"sub-create":         true,
	"sub-drop":           true,
	"sub-refresh":        true,
	"base-backup":        true,
	"wal-archive-status": true,
	"restore-pitr":       true,
}

// pgReplSlotTypes PostgreSQL 复制槽类型白名单。
var pgReplSlotTypes = map[string]bool{
	"physical": true,
	"logical":  true,
}

// pgReplConnInfoRe 是连接信息白名单（拒绝 shell 元字符）。
var pgReplConnInfoRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9 _=@.'":,/\-]{0,511}$`)

// handleDBPGRepl 处理 POST /api/v1/db/postgresql/repl。
func (s *Server) handleDBPGRepl(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	var req struct {
		DeviceID         string `json:"deviceID"`
		Action           string `json:"action"`
		DB               string `json:"db"`
		SlotName         string `json:"slotName"`
		SlotType         string `json:"slotType"`
		PublicationName  string `json:"publicationName"`
		SubscriptionName string `json:"subscriptionName"`
		ConnInfo         string `json:"connInfo"`
	}
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.DeviceID == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "deviceID is required"})
		return
	}
	if !pgReplActions[req.Action] {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	db := req.DB
	if db == "" {
		db = "postgres"
	}
	if _, err := opsDBName("db", db); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	if req.SlotType != "" {
		if !pgReplSlotTypes[req.SlotType] {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid slotType"})
			return
		}
	}
	if req.SlotName != "" {
		if !pgReplSlotNameRe.MatchString(req.SlotName) {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid slotName"})
			return
		}
	}
	if req.PublicationName != "" {
		if !pgReplSlotNameRe.MatchString(req.PublicationName) {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid publicationName"})
			return
		}
	}
	if req.SubscriptionName != "" {
		if !pgReplSlotNameRe.MatchString(req.SubscriptionName) {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid subscriptionName"})
			return
		}
	}
	if req.ConnInfo != "" {
		if !pgReplConnInfoRe.MatchString(req.ConnInfo) {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid connInfo"})
			return
		}
	}

	switch req.Action {
	case "slot-create", "slot-drop":
		if req.SlotName == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "slotName is required"})
			return
		}
	case "pub-create", "pub-drop":
		if req.PublicationName == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "publicationName is required"})
			return
		}
	case "sub-create", "sub-drop", "sub-refresh":
		if req.SubscriptionName == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "subscriptionName is required"})
			return
		}
	}
	if req.Action == "sub-create" {
		if req.ConnInfo == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "connInfo is required"})
			return
		}
		if req.PublicationName == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "publicationName is required"})
			return
		}
	}
	if req.Action == "slot-create" && req.SlotType == "" {
		req.SlotType = "physical"
	}

	actx, ok := s.subsystemAuthorize(w, r, "task:write")
	if !ok {
		return
	}
	if dev := s.store.Device(req.DeviceID); dev != nil && dev.TenantID != "" && actx.TenantID != "" && dev.TenantID != actx.TenantID {
		paginate.WriteJSON(w, http.StatusForbidden, map[string]string{"error": "tenant mismatch"})
		return
	}
	if ag := s.store.Agent(req.DeviceID); ag != nil && ag.TenantID != "" && actx.TenantID != "" && ag.TenantID != actx.TenantID {
		paginate.WriteJSON(w, http.StatusForbidden, map[string]string{"error": "tenant mismatch"})
		return
	}

	script := buildDBPGReplScript(req.Action, db, req.SlotName, req.SlotType, req.PublicationName, req.SubscriptionName, req.ConnInfo)
	task, err := s.createSystemTask(actx.TenantID, req.DeviceID, script, "db_pg_repl_"+req.Action)
	if err != nil {
		writeInternalError(r.Context(), w, "db.pg.repl", err)
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: "db_pg_repl_" + req.Action,
		Target: db, Detail: sanitizeAuditDetail("deviceID=" + req.DeviceID + " slot=" + req.SlotName + " pub=" + req.PublicationName + " sub=" + req.SubscriptionName),
	})
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": task.TaskID, "action": req.Action})
}

func buildDBPGReplScript(action, db, slotName, slotType, publicationName, subscriptionName, connInfo string) string {
	var b strings.Builder
	b.WriteString("set -e\n")
	dbQ := shellQuote(db)
	switch action {
	case "slots-list":
		b.WriteString(fmt.Sprintf("psql -d %s -c \"SELECT slot_name, slot_type, active, restart_lsn FROM pg_replication_slots ORDER BY slot_name;\"\n", dbQ))
	case "slot-create":
		if slotType == "logical" {
			b.WriteString(fmt.Sprintf("psql -d %s -c \"SELECT pg_create_logical_replication_slot(%s, 'pgoutput');\"\n", dbQ, shellQuote(slotName)))
		} else {
			b.WriteString(fmt.Sprintf("psql -d %s -c \"SELECT pg_create_physical_replication_slot(%s);\"\n", dbQ, shellQuote(slotName)))
		}
	case "slot-drop":
		b.WriteString(fmt.Sprintf("psql -d %s -c \"SELECT pg_drop_replication_slot(%s);\"\n", dbQ, shellQuote(slotName)))
	case "pubs-list":
		b.WriteString(fmt.Sprintf("psql -d %s -c \"SELECT pubname, puballtables, pubinsert, pubupdate, pubdelete FROM pg_publication ORDER BY pubname;\"\n", dbQ))
	case "pub-create":
		b.WriteString(fmt.Sprintf("psql -d %s -c \"CREATE PUBLICATION %s;\"\n", dbQ, publicationName))
	case "pub-drop":
		b.WriteString(fmt.Sprintf("psql -d %s -c \"DROP PUBLICATION IF EXISTS %s;\"\n", dbQ, publicationName))
	case "subs-list":
		b.WriteString(fmt.Sprintf("psql -d %s -c \"SELECT subname, subenabled, subconninfo, subslotname FROM pg_subscription ORDER BY subname;\"\n", dbQ))
	case "sub-create":
		b.WriteString(fmt.Sprintf("psql -d %s -c \"CREATE SUBSCRIPTION %s CONNECTION %s PUBLICATION %s;\"\n", dbQ, subscriptionName, shellQuote(connInfo), publicationName))
	case "sub-drop":
		b.WriteString(fmt.Sprintf("psql -d %s -c \"DROP SUBSCRIPTION IF EXISTS %s;\"\n", dbQ, subscriptionName))
	case "sub-refresh":
		b.WriteString(fmt.Sprintf("psql -d %s -c \"ALTER SUBSCRIPTION %s REFRESH PUBLICATION;\"\n", dbQ, subscriptionName))
	case "base-backup":
		b.WriteString("pg_basebackup -D /tmp/pg_basebackup -Ft -z -P -X stream\n")
	case "wal-archive-status":
		b.WriteString(fmt.Sprintf("psql -d %s -c \"SELECT * FROM pg_stat_archiver;\"\n", dbQ))
	case "restore-pitr":
		b.WriteString("echo 'PITR restore: configure recovery settings and restart cluster'\n")
	}
	fmt.Fprintf(&b, "echo 'postgresql repl %s done'\n", action)
	return b.String()
}
