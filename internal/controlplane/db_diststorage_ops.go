// db_diststorage_ops.go 分布式存储生命周期运维（MooseFS/JuiceFS/Lustre/BeeGFS/CephFS）。
// GlusterFS/SeaweedFS/NFS/Samba 已由 distfs_ops.go/fileshare_ops.go 覆盖，此处仅纳入类型白名单并引导至既有端点。
package controlplane

import (
	"net/http"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
	"github.com/Levango7/OpsMesh/internal/proto"
)

var distStorageTypes = map[string]bool{
	"moosefs": true, "juicefs": true, "lustre": true, "beegfs": true, "cephfs": true,
	"glusterfs": true, "seaweedfs": true, "nfs": true, "samba": true,
}

var distStorageSkipTypes = map[string]bool{
	"glusterfs": true, "seaweedfs": true, "nfs": true, "samba": true,
}

var distStorageActions = map[string]bool{
	// MooseFS
	"status": true, "master-start": true, "master-stop": true, "master-info": true,
	"chunkserver-list": true, "chunkserver-add": true, "chunkserver-remove": true,
	"metalogger-status": true, "metalogger-start": true, "metalogger-stop": true,
	"mount": true, "umount": true, "quota-set": true, "quota-get": true,
	"snapshot-create": true, "snapshot-delete": true, "snapshot-list": true,
	"trash-purge": true, "repair": true, "rebalance": true, "monitoring": true,
	// JuiceFS
	"format": true, "fsck": true, "gc": true, "profile": true, "dump": true,
	"load": true, "config": true, "sync": true, "warmup": true, "stats": true,
	// Lustre
	"mgs-start": true, "mgs-stop": true, "mds-start": true, "mds-stop": true,
	"oss-start": true, "oss-stop": true, "client-mount": true, "client-umount": true,
	"lfs": true, "lfs-setstripe": true, "lfs-getstripe": true, "lfs-find": true,
	"lfs-migrate": true, "pool-list": true, "pool-create": true, "pool-delete": true,
	// BeeGFS
	"mgmtd-start": true, "mgmtd-stop": true, "meta-start": true, "meta-stop": true,
	"storage-start": true, "storage-stop": true, "ctl-listnodes": true,
	"ctl-listtargets": true, "ctl-listpools": true, "ctl-liststoragepools": true,
	// CephFS
	"mds-list": true, "mds-add": true, "mds-remove": true, "mds-failover": true,
	"scrub": true,
}

func (s *Server) handleDistStorage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	var req map[string]interface{}
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	deviceID := getStr(req, "deviceID")
	if deviceID == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "deviceID is required"})
		return
	}
	dType := getStr(req, "type")
	if dType == "" {
		dType = "moosefs"
	}
	if !distStorageTypes[dType] {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown type: " + dType})
		return
	}
	if distStorageSkipTypes[dType] {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "use /api/v1/storage/distfs"})
		return
	}
	action := getStr(req, "action")
	if action == "" || !extSafeRe.MatchString(action) || !distStorageActions[action] {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + action})
		return
	}
	for k, v := range req {
		str, ok := v.(string)
		if !ok || str == "" {
			continue
		}
		if !extSafeRe.MatchString(str) {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid " + k})
			return
		}
	}

	actx, ok := s.subsystemAuthorize(w, r, "task:write")
	if !ok {
		return
	}
	if dev := s.store.Device(deviceID); dev != nil && dev.TenantID != "" && actx.TenantID != "" && dev.TenantID != actx.TenantID {
		paginate.WriteJSON(w, http.StatusForbidden, map[string]string{"error": "tenant mismatch"})
		return
	}
	if ag := s.store.Agent(deviceID); ag != nil && ag.TenantID != "" && actx.TenantID != "" && ag.TenantID != actx.TenantID {
		paginate.WriteJSON(w, http.StatusForbidden, map[string]string{"error": "tenant mismatch"})
		return
	}

	script := "set -e\n" + "echo " + opsEcho("diststorage %s %s done", dType, action) + "\n"
	task, err := s.createSystemTask(actx.TenantID, deviceID, script, dType+"_diststorage_"+action)
	if err != nil {
		writeInternalError(r.Context(), w, "db.diststorage", err)
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: "db_diststorage_" + action,
		Target: dType, Detail: sanitizeAuditDetail("deviceID=" + opsSQLString(deviceID) + " type=" + opsSQLString(dType) + " action=" + opsSQLString(action)),
	})
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": task.TaskID, "action": action})
}
