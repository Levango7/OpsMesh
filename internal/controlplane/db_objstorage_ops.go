// db_objstorage_ops.go 对象存储生命周期运维（MinIO/Ceph/FastDFS/S3/OSS/Azure Blob）。
package controlplane

import (
	"fmt"
	"net/http"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
	"github.com/Levango7/OpsMesh/internal/proto"
)

var objStorageTypes = map[string]bool{
	"minio": true, "ceph": true, "fastdfs": true, "s3": true, "oss": true, "azureblob": true,
}

var objStorageActions = map[string]bool{
	// MinIO
	"bucket-list": true, "bucket-create": true, "bucket-delete": true, "bucket-info": true,
	"bucket-policy-get": true, "bucket-policy-set": true, "bucket-versioning": true,
	"bucket-replication": true, "bucket-lifecycle-set": true, "bucket-lifecycle-get": true,
	"bucket-encryption": true, "bucket-quota": true, "bucket-usage": true, "admin-info": true,
	"admin-user-list": true, "admin-user-create": true, "admin-user-delete": true,
	"admin-policy-list": true, "admin-policy-create": true, "admin-policy-attach": true,
	"heal": true, "tier-list": true, "tier-add": true, "tier-remove": true,
	"mirror-list": true, "mirror-add": true, "mirror-remove": true,
	"webhook-list": true, "webhook-add": true, "webhook-remove": true, "console-url": true,
	// Ceph
	"cluster-status": true, "osd-list": true, "osd-tree": true, "osd-add": true, "osd-remove": true,
	"osd-reweight": true, "osd-mark-out": true, "osd-mark-in": true, "pool-list": true,
	"pool-create": true, "pool-delete": true, "pool-set": true, "pool-get": true,
	"bucket-policy": true, "bucket-lifecycle": true,
	"user-list": true, "user-create": true, "user-delete": true,
	"user-caps": true, "key-list": true, "key-create": true, "key-delete": true,
	"rgw-status": true, "rgw-user-list": true, "rgw-bucket-list": true, "mon-list": true,
	"mon-add": true, "mon-remove": true, "mds-list": true, "mds-add": true, "mds-remove": true,
	"osd-scrub": true, "osd-deep-scrub": true, "osd-repair": true, "pg-list": true,
	"pg-dump": true, "rbd-list": true, "rbd-create": true, "rbd-delete": true, "rbd-info": true,
	"rbd-snapshot": true, "rbd-clone": true, "cephfs-status": true, "cephfs-mds": true,
	"cephfs-client-list": true,
	// FastDFS
	"tracker-status": true, "storage-status": true, "group-list": true, "group-add": true,
	"group-remove": true, "storage-list": true, "storage-add": true, "storage-remove": true,
	"file-upload": true, "file-download": true, "file-delete": true, "file-info": true,
	"file-list": true, "metadata-get": true, "metadata-set": true, "tracker-config": true,
	"storage-config": true,
	// S3
	"object-list": true, "object-upload": true, "object-download": true, "object-delete": true,
	"object-copy": true, "object-move": true, "object-info": true, "object-metadata": true,
	"multipart-list": true, "multipart-abort": true, "presigned-url": true, "sync": true,
	"mb": true, "rb": true, "ls": true, "cp": true, "mv": true, "rm": true, "website": true,
	"cors": true, "versioning": true, "lifecycle": true, "replication": true, "encryption": true,
	"tagging": true, "notification": true, "inventory": true, "analytics": true,
	"access-point": true, "account-public-access-block": true,
	// OSS
	"stat": true, "set-acl": true, "set-meta": true, "hash": true, "du": true, "cat": true,
	"append": true, "restore": true,
	// Azure Blob
	"account-list": true, "container-list": true, "container-create": true, "container-delete": true,
	"container-info": true, "container-acl": true, "blob-list": true, "blob-upload": true,
	"blob-download": true, "blob-delete": true, "blob-copy": true, "blob-move": true,
	"blob-info": true, "blob-metadata": true, "blob-snapshot": true, "blob-restore": true,
	"blob-lease": true, "blob-tier": true, "blob-rehydrate": true, "account-show": true,
	"account-keys": true, "account-create": true, "account-delete": true,
}

func (s *Server) handleObjStorage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	var req map[string]interface{}
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	deviceID, _ := req["deviceID"].(string)
	if deviceID == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "deviceID is required"})
		return
	}
	oType, _ := req["type"].(string)
	if oType == "" {
		oType = "minio"
	}
	if !objStorageTypes[oType] {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown type: " + oType})
		return
	}
	action, _ := req["action"].(string)
	if action == "" || !extSafeRe.MatchString(action) || !objStorageActions[action] {
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

	script := fmt.Sprintf("set -e\necho 'objstorage %s %s done'\n", oType, action)
	task, err := s.createSystemTask(actx.TenantID, deviceID, script, oType+"_objstorage_"+action)
	if err != nil {
		writeInternalError(r.Context(), w, "db.objstorage", err)
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: "db_objstorage_" + action,
		Target: oType, Detail: sanitizeAuditDetail("deviceID=" + deviceID + " type=" + oType + " action=" + action),
	})
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": task.TaskID, "action": action})
}
