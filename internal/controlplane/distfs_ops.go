// Package controlplane distfs_ops.go 实现分布式文件系统运维
// 支持：Ceph (RADOS/CephFS/RGW)、GlusterFS、HDFS、SeaweedFS
package controlplane

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
)

// handleDistFS 处理 POST /api/v1/storage/distfs
func (s *Server) handleDistFS(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Type     string `json:"type"`   // ceph/glusterfs/hdfs/seaweedfs
		Action   string `json:"action"` // deploy/status/create-pool/create-volume/mount/upload/download/list/delete
		Mode     string `json:"mode"`   // single/cluster
		Version  string `json:"version"`
		// Ceph specific
		Pool  string `json:"pool"`
		PGNum int    `json:"pgNum"`
		// GlusterFS specific
		Volume string `json:"volume"`
		Bricks string `json:"bricks"`
		// HDFS specific
		Path string `json:"path"`
		// Common
		File   string `json:"file"`
		Remote string `json:"remote"`
		Host   string `json:"host"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	if req.Type == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "type is required (ceph/glusterfs/hdfs/seaweedfs)"})
		return
	}

	switch req.Type {
	case "ceph":
		s.handleCeph(w, r, req)
	case "glusterfs":
		s.handleGlusterFS(w, r, req)
	case "hdfs":
		s.handleHDFS(w, r, req)
	case "seaweedfs":
		s.handleSeaweedFS(w, r, req)
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown type: " + req.Type})
	}
}

// ============================================================================
// Ceph
// ============================================================================

func (s *Server) handleCeph(w http.ResponseWriter, r *http.Request, req struct {
	DeviceID string `json:"deviceID"`
	Type     string `json:"type"`
	Action   string `json:"action"`
	Mode     string `json:"mode"`
	Version  string `json:"version"`
	Pool     string `json:"pool"`
	PGNum    int    `json:"pgNum"`
	Volume   string `json:"volume"`
	Bricks   string `json:"bricks"`
	Path     string `json:"path"`
	File     string `json:"file"`
	Remote   string `json:"remote"`
	Host     string `json:"host"`
}) {
	switch req.Action {
	case "deploy":
		version := req.Version
		if version == "" {
			version = "18.2.0" // Reef
		}
		script := fmt.Sprintf(`cephadm bootstrap --mon-ip 127.0.0.1 --initial-dashboard-user admin --initial-dashboard-password admin 2>/dev/null || (apt-get install -y ceph ceph-mgr ceph-mon ceph-osd 2>/dev/null && echo "Ceph %s deployed (manual mode)")`, version)
		taskID, ok := s.dispatchFastDFSTask(w, r, req.DeviceID, script, "ceph_deploy")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "deploy"})

	case "status":
		script := `ceph -s 2>/dev/null || echo "Ceph not running" && ceph osd tree 2>/dev/null | head -20`
		taskID, ok := s.dispatchFastDFSTask(w, r, req.DeviceID, script, "ceph_status")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "status"})

	case "create-pool":
		if req.Pool == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "pool is required"})
			return
		}
		pgNum := req.PGNum
		if pgNum <= 0 {
			pgNum = 32
		}
		script := fmt.Sprintf(`ceph osd pool create %s %d 2>/dev/null && ceph osd pool application enable %s rbd 2>/dev/null && echo "Pool %s created (pg=%d)"`, req.Pool, pgNum, req.Pool, req.Pool, pgNum)
		taskID, ok := s.dispatchFastDFSTask(w, r, req.DeviceID, script, "ceph_create-pool")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "create-pool"})

	case "delete-pool":
		if req.Pool == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "pool is required"})
			return
		}
		script := fmt.Sprintf(`ceph osd pool delete %s %s --yes-i-really-really-mean-it 2>/dev/null && echo "Pool %s deleted"`, req.Pool, req.Pool, req.Pool)
		taskID, ok := s.dispatchFastDFSTask(w, r, req.DeviceID, script, "ceph_delete-pool")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "delete-pool"})

	case "list-pools":
		script := `ceph osd lspools 2>/dev/null || echo "Ceph not available"`
		taskID, ok := s.dispatchFastDFSTask(w, r, req.DeviceID, script, "ceph_list-pools")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "list-pools"})

	case "cephfs-create":
		if req.Volume == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "volume (filesystem name) is required"})
			return
		}
		script := fmt.Sprintf(`ceph fs new %s %s_data %s_meta 2>/dev/null && echo "CephFS %s created"`, req.Volume, req.Volume, req.Volume, req.Volume)
		taskID, ok := s.dispatchFastDFSTask(w, r, req.DeviceID, script, "ceph_cephfs-create")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "cephfs-create"})

	case "rbd-create":
		if req.Pool == "" || req.Volume == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "pool and volume (image name) are required"})
			return
		}
		script := fmt.Sprintf(`rbd create %s/%s --size 10240 2>/dev/null && echo "RBD image %s/%s created (10GB)"`, req.Pool, req.Volume, req.Pool, req.Volume)
		taskID, ok := s.dispatchFastDFSTask(w, r, req.DeviceID, script, "ceph_rbd-create")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "rbd-create"})

	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
	}
}

// ============================================================================
// GlusterFS
// ============================================================================

func (s *Server) handleGlusterFS(w http.ResponseWriter, r *http.Request, req struct {
	DeviceID string `json:"deviceID"`
	Type     string `json:"type"`
	Action   string `json:"action"`
	Mode     string `json:"mode"`
	Version  string `json:"version"`
	Pool     string `json:"pool"`
	PGNum    int    `json:"pgNum"`
	Volume   string `json:"volume"`
	Bricks   string `json:"bricks"`
	Path     string `json:"path"`
	File     string `json:"file"`
	Remote   string `json:"remote"`
	Host     string `json:"host"`
}) {
	switch req.Action {
	case "deploy":
		version := req.Version
		if version == "" {
			version = "11.0"
		}
		script := fmt.Sprintf(`apt-get install -y glusterfs-server 2>/dev/null || yum install -y glusterfs-server 2>/dev/null && systemctl enable glusterd && systemctl start glusterd && echo "GlusterFS %s deployed"`, version)
		taskID, ok := s.dispatchFastDFSTask(w, r, req.DeviceID, script, "glusterfs_deploy")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "deploy"})

	case "status":
		script := `gluster volume status 2>/dev/null || echo "GlusterFS not running" && gluster peer status 2>/dev/null`
		taskID, ok := s.dispatchFastDFSTask(w, r, req.DeviceID, script, "glusterfs_status")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "status"})

	case "create-volume":
		if req.Volume == "" || req.Bricks == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "volume and bricks are required"})
			return
		}
		volType := req.Mode
		if volType == "" {
			volType = "replica 3"
		}
		script := fmt.Sprintf(`gluster volume create %s %s %s force 2>/dev/null && gluster volume start %s && echo "Volume %s created (%s)"`, req.Volume, volType, req.Bricks, req.Volume, req.Volume, volType)
		taskID, ok := s.dispatchFastDFSTask(w, r, req.DeviceID, script, "glusterfs_create-volume")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "create-volume"})

	case "delete-volume":
		if req.Volume == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "volume is required"})
			return
		}
		script := fmt.Sprintf(`gluster volume stop %s 2>/dev/null && gluster volume delete %s 2>/dev/null && echo "Volume %s deleted"`, req.Volume, req.Volume, req.Volume)
		taskID, ok := s.dispatchFastDFSTask(w, r, req.DeviceID, script, "glusterfs_delete-volume")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "delete-volume"})

	case "list-volumes":
		script := `gluster volume list 2>/dev/null || echo "No volumes"`
		taskID, ok := s.dispatchFastDFSTask(w, r, req.DeviceID, script, "glusterfs_list-volumes")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "list-volumes"})

	case "peer-probe":
		if req.Host == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "host is required"})
			return
		}
		script := fmt.Sprintf(`gluster peer probe %s 2>/dev/null && echo "Peer %s probed"`, req.Host, req.Host)
		taskID, ok := s.dispatchFastDFSTask(w, r, req.DeviceID, script, "glusterfs_peer-probe")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "peer-probe"})

	case "mount":
		if req.Volume == "" || req.Path == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "volume and path are required"})
			return
		}
		script := fmt.Sprintf(`mkdir -p %s && mount -t glusterfs localhost:/%s %s 2>/dev/null && echo "Mounted %s at %s"`, req.Path, req.Volume, req.Path, req.Volume, req.Path)
		taskID, ok := s.dispatchFastDFSTask(w, r, req.DeviceID, script, "glusterfs_mount")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "mount"})

	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
	}
}

// ============================================================================
// HDFS
// ============================================================================

func (s *Server) handleHDFS(w http.ResponseWriter, r *http.Request, req struct {
	DeviceID string `json:"deviceID"`
	Type     string `json:"type"`
	Action   string `json:"action"`
	Mode     string `json:"mode"`
	Version  string `json:"version"`
	Pool     string `json:"pool"`
	PGNum    int    `json:"pgNum"`
	Volume   string `json:"volume"`
	Bricks   string `json:"bricks"`
	Path     string `json:"path"`
	File     string `json:"file"`
	Remote   string `json:"remote"`
	Host     string `json:"host"`
}) {
	switch req.Action {
	case "deploy":
		version := req.Version
		if version == "" {
			version = "3.3.6"
		}
		script := fmt.Sprintf(`wget -q https://archive.apache.org/dist/hadoop/common/hadoop-%s/hadoop-%s.tar.gz -O /tmp/hadoop.tgz && tar -xzf /tmp/hadoop.tgz -C /opt/ && ln -sfn /opt/hadoop-%s /opt/hadoop && echo "Hadoop %s deployed"`, version, version, version, version)
		taskID, ok := s.dispatchFastDFSTask(w, r, req.DeviceID, script, "hdfs_deploy")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "deploy"})

	case "status":
		script := `/opt/hadoop/bin/hdfs dfsadmin -report 2>/dev/null | head -20 || echo "HDFS not running"`
		taskID, ok := s.dispatchFastDFSTask(w, r, req.DeviceID, script, "hdfs_status")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "status"})

	case "mkdir":
		if req.Path == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "path is required"})
			return
		}
		script := fmt.Sprintf(`/opt/hadoop/bin/hdfs dfs -mkdir -p %s 2>/dev/null && echo "Directory %s created"`, req.Path, req.Path)
		taskID, ok := s.dispatchFastDFSTask(w, r, req.DeviceID, script, "hdfs_mkdir")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "mkdir"})

	case "ls":
		path := req.Path
		if path == "" {
			path = "/"
		}
		script := fmt.Sprintf(`/opt/hadoop/bin/hdfs dfs -ls %s 2>/dev/null || echo "Path not found"`, path)
		taskID, ok := s.dispatchFastDFSTask(w, r, req.DeviceID, script, "hdfs_ls")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "ls"})

	case "put":
		if req.File == "" || req.Path == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "file and path are required"})
			return
		}
		script := fmt.Sprintf(`/opt/hadoop/bin/hdfs dfs -put %s %s 2>/dev/null && echo "File uploaded to %s"`, req.File, req.Path, req.Path)
		taskID, ok := s.dispatchFastDFSTask(w, r, req.DeviceID, script, "hdfs_put")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "put"})

	case "get":
		if req.Remote == "" || req.File == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "remote and file are required"})
			return
		}
		script := fmt.Sprintf(`/opt/hadoop/bin/hdfs dfs -get %s %s 2>/dev/null && echo "File downloaded to %s"`, req.Remote, req.File, req.File)
		taskID, ok := s.dispatchFastDFSTask(w, r, req.DeviceID, script, "hdfs_get")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "get"})

	case "rm":
		if req.Path == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "path is required"})
			return
		}
		script := fmt.Sprintf(`/opt/hadoop/bin/hdfs dfs -rm -r %s 2>/dev/null && echo "Deleted %s"`, req.Path, req.Path)
		taskID, ok := s.dispatchFastDFSTask(w, r, req.DeviceID, script, "hdfs_rm")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "rm"})

	case "du":
		path := req.Path
		if path == "" {
			path = "/"
		}
		script := fmt.Sprintf(`/opt/hadoop/bin/hdfs dfs -du -h %s 2>/dev/null | head -20`, path)
		taskID, ok := s.dispatchFastDFSTask(w, r, req.DeviceID, script, "hdfs_du")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "du"})

	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
	}
}

// ============================================================================
// SeaweedFS
// ============================================================================

func (s *Server) handleSeaweedFS(w http.ResponseWriter, r *http.Request, req struct {
	DeviceID string `json:"deviceID"`
	Type     string `json:"type"`
	Action   string `json:"action"`
	Mode     string `json:"mode"`
	Version  string `json:"version"`
	Pool     string `json:"pool"`
	PGNum    int    `json:"pgNum"`
	Volume   string `json:"volume"`
	Bricks   string `json:"bricks"`
	Path     string `json:"path"`
	File     string `json:"file"`
	Remote   string `json:"remote"`
	Host     string `json:"host"`
}) {
	switch req.Action {
	case "deploy":
		version := req.Version
		if version == "" {
			version = "3.66"
		}
		script := fmt.Sprintf(`wget -q https://github.com/seaweedfs/seaweedfs/releases/download/%s/linux_amd64.tar.gz -O /tmp/seaweedfs.tgz && tar -xzf /tmp/seaweedfs.tgz -C /usr/local/bin/ && echo "SeaweedFS %s deployed"`, version, version)
		taskID, ok := s.dispatchFastDFSTask(w, r, req.DeviceID, script, "seaweedfs_deploy")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "deploy"})

	case "start-master":
		script := `weed master -ip=0.0.0.0 -port=9333 -mdir=/data/seaweedfs/master -defaultReplication=001 & echo "Master started"`
		taskID, ok := s.dispatchFastDFSTask(w, r, req.DeviceID, script, "seaweedfs_start-master")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "start-master"})

	case "start-volume":
		script := `weed volume -ip=0.0.0.0 -port=8080 -dir=/data/seaweedfs/volume -max=100 -mserver=localhost:9333 & echo "Volume server started"`
		taskID, ok := s.dispatchFastDFSTask(w, r, req.DeviceID, script, "seaweedfs_start-volume")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "start-volume"})

	case "upload":
		if req.File == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "file is required"})
			return
		}
		script := fmt.Sprintf(`weed upload %s 2>/dev/null && echo "Uploaded %s"`, req.File, req.File)
		taskID, ok := s.dispatchFastDFSTask(w, r, req.DeviceID, script, "seaweedfs_upload")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "upload"})

	case "download":
		if req.Remote == "" || req.File == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "remote and file are required"})
			return
		}
		script := fmt.Sprintf(`weed download -o %s %s 2>/dev/null && echo "Downloaded to %s"`, req.File, req.Remote, req.File)
		taskID, ok := s.dispatchFastDFSTask(w, r, req.DeviceID, script, "seaweedfs_download")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "download"})

	case "status":
		script := `weed version 2>/dev/null || echo "SeaweedFS not installed" && curl -s http://localhost:9333/cluster/status 2>/dev/null || echo "Master not running"`
		taskID, ok := s.dispatchFastDFSTask(w, r, req.DeviceID, script, "seaweedfs_status")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "status"})

	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
	}
}

// ============================================================================
// 路由注册
// ============================================================================

// registerDistFSRoutes 注册分布式文件系统路由
func (s *Server) registerDistFSRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/storage/distfs", s.handleDistFS)
}
