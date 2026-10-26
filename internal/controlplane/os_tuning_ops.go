// Package controlplane os_tuning_ops.go 实现操作系统深度运维与调优
// 覆盖：硬件盘点、内核调优、应用调优、存储调优、网络调优、安全加固、资源管理、证书管理、系统备份
package controlplane

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
)

// ============================================================================
// 硬件盘点 (CPU/内存/磁盘/网络/GPU 全面采集)
// ============================================================================

// handleHardwareInventoryFull 处理 POST /api/v1/os/hardware/inventory-full
func (s *Server) handleHardwareInventoryFull(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	script := `echo "=== CPU ===" && lscpu 2>/dev/null && echo "=== Memory ===" && free -h 2>/dev/null && echo "=== Disks ===" && lsblk -d -o NAME,SIZE,MODEL,TYPE,SERIAL,ROTA 2>/dev/null && echo "=== Network ===" && ip -brief addr 2>/dev/null && echo "=== GPU ===" && (nvidia-smi --query-gpu=index,name,driver_version,memory.total --format=csv 2>/dev/null || lspci | grep -i vga) && echo "=== Motherboard ===" && dmidecode -t baseboard 2>/dev/null | head -10 && echo "=== Chassis ===" && dmidecode -t chassis 2>/dev/null | head -10 && echo "=== BIOS ===" && dmidecode -t bios 2>/dev/null | head -10`
	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "os_hardware_inventory_full")
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "hardware_inventory_full"})
}

// handleHardwareSensors 处理 POST /api/v1/os/hardware/sensors
func (s *Server) handleHardwareSensors(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // ipmi/smart/nvme
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	var script string
	switch req.Action {
	case "ipmi":
		script = `ipmitool sensor list 2>/dev/null || echo "IPMI not available"`
	case "smart":
		script = `for d in /dev/sd* /dev/nvme*n1; do [ -b "$d" ] && echo "=== $d ===" && smartctl -a "$d" 2>/dev/null | grep -i "temperature\|health\|reallocated\|pending\|uncorrectable" ; done`
	case "nvme":
		script = `nvme smart-log /dev/nvme0 2>/dev/null || echo "NVMe not available"`
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "os_hardware_sensors_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// ============================================================================
// 内核调优 (sysctl, ulimit, kernel params)
// ============================================================================

// handleKernelTuning 处理 POST /api/v1/os/kernel/tuning
func (s *Server) handleKernelTuning(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // get/set/apply-preset
		Preset   string `json:"preset"` // database/web/high-concurrency/low-latency
		Param    string `json:"param"`  // sysctl key
		Value    string `json:"value"`  // sysctl value
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	switch req.Action {
	case "get":
		var script string
		if req.Param != "" {
			script = fmt.Sprintf(`sysctl %s 2>/dev/null || echo "Param not found"`, req.Param)
		} else {
			script = `sysctl -a 2>/dev/null | head -50`
		}
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "os_kernel_tuning_get")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "get"})

	case "set":
		if req.Param == "" || req.Value == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "param and value are required"})
			return
		}
		script := fmt.Sprintf(`sysctl -w %s=%s 2>/dev/null && echo "%s = %s" >> /etc/sysctl.d/99-opsmesh.conf && sysctl -p /etc/sysctl.d/99-opsmesh.conf 2>/dev/null && echo "Applied"`, req.Param, req.Value, req.Param, req.Value)
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "os_kernel_tuning_set")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "set"})

	case "apply-preset":
		preset := req.Preset
		if preset == "" {
			preset = "database"
		}
		script := buildKernelTuningPreset(preset)
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "os_kernel_tuning_apply-preset")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "apply-preset"})

	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
	}
}

func buildKernelTuningPreset(preset string) string {
	switch preset {
	case "database":
		return `cat > /etc/sysctl.d/99-opsmesh-db.conf << 'EOF'
# Database tuning
vm.swappiness = 10
vm.dirty_ratio = 15
vm.dirty_background_ratio = 5
vm.dirty_expire_centisecs = 500
vm.dirty_writeback_centisecs = 100
net.core.somaxconn = 65535
net.ipv4.tcp_max_syn_backlog = 65535
net.ipv4.tcp_tw_reuse = 1
net.ipv4.ip_local_port_range = 1024 65535
fs.file-max = 2097152
fs.aio-max-nr = 1048576
EOF
sysctl -p /etc/sysctl.d/99-opsmesh-db.conf && echo "Database tuning applied"`
	case "web":
		return `cat > /etc/sysctl.d/99-opsmesh-web.conf << 'EOF'
# Web server tuning
net.core.somaxconn = 65535
net.ipv4.tcp_max_syn_backlog = 65535
net.ipv4.tcp_tw_reuse = 1
net.ipv4.tcp_fin_timeout = 15
net.ipv4.tcp_keepalive_time = 300
net.ipv4.tcp_keepalive_probes = 5
net.ipv4.tcp_keepalive_intvl = 15
net.core.netdev_max_backlog = 65535
fs.file-max = 2097152
EOF
sysctl -p /etc/sysctl.d/99-opsmesh-web.conf && echo "Web tuning applied"`
	case "high-concurrency":
		return `cat > /etc/sysctl.d/99-opsmesh-hc.conf << 'EOF'
# High concurrency tuning
net.core.somaxconn = 65535
net.ipv4.tcp_max_syn_backlog = 65535
net.core.netdev_max_backlog = 65535
net.ipv4.tcp_tw_reuse = 1
net.ipv4.tcp_fin_timeout = 10
net.ipv4.ip_local_port_range = 1024 65535
fs.file-max = 2097152
fs.nr_open = 2097152
vm.max_map_count = 262144
EOF
sysctl -p /etc/sysctl.d/99-opsmesh-hc.conf && echo "High concurrency tuning applied"`
	case "low-latency":
		return `cat > /etc/sysctl.d/99-opsmesh-ll.conf << 'EOF'
# Low latency tuning
net.core.somaxconn = 65535
net.ipv4.tcp_low_latency = 1
net.ipv4.tcp_tw_reuse = 1
net.ipv4.tcp_fin_timeout = 5
net.ipv4.tcp_keepalive_time = 120
net.core.netdev_max_backlog = 65535
net.ipv4.tcp_rmem = 4096 87380 16777216
net.ipv4.tcp_wmem = 4096 65536 16777216
EOF
sysctl -p /etc/sysctl.d/99-opsmesh-ll.conf && echo "Low latency tuning applied"`
	default:
		return `echo "Unknown preset: ` + preset + `"`
	}
}

// handleOSKernelModule 处理 POST /api/v1/os/kernel/module
func (s *Server) handleOSKernelModule(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // list/load/unload/info
		Module   string `json:"module"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	var script string
	switch req.Action {
	case "list":
		script = `lsmod 2>/dev/null | head -50 || echo "lsmod not available"`
	case "load":
		if req.Module == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "module is required"})
			return
		}
		script = fmt.Sprintf(`modprobe %s 2>/dev/null && echo "Module %s loaded" || echo "Failed to load module %s"`, req.Module, req.Module, req.Module)
	case "unload":
		if req.Module == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "module is required"})
			return
		}
		script = fmt.Sprintf(`modprobe -r %s 2>/dev/null && echo "Module %s unloaded" || echo "Failed to unload module %s"`, req.Module, req.Module, req.Module)
	case "info":
		if req.Module == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "module is required"})
			return
		}
		script = fmt.Sprintf(`modinfo %s 2>/dev/null || echo "Module info not available"`, req.Module)
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "os_kernel_module_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// ============================================================================
// 应用调优 (JVM, 数据库, Web 服务器)
// ============================================================================

// handleAppTuning 处理 POST /api/v1/os/app-tuning
func (s *Server) handleAppTuning(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		App      string `json:"app"`      // jvm/mysql/nginx/redis/es
		Action   string `json:"action"`  // analyze/recommend/apply
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	switch req.Action {
	case "analyze":
		var script string
		switch req.App {
		case "jvm":
			script = `echo "=== JVM Analysis ===" && (jps 2>/dev/null && echo "---" && jstat -gcutil $(pgrep -f java | head -1) 2>/dev/null || echo "JVM not found")`
		case "mysql":
			script = `echo "=== MySQL Analysis ===" && mysql -e "SHOW VARIABLES LIKE 'innodb%';" 2>/dev/null | head -20 && echo "---" && mysql -e "SHOW GLOBAL STATUS LIKE 'Threads%';" 2>/dev/null`
		case "nginx":
			script = `echo "=== Nginx Analysis ===" && nginx -T 2>/dev/null | grep -i "worker\|keepalive\|gzip" | head -20`
		case "redis":
			script = `echo "=== Redis Analysis ===" && redis-cli info memory 2>/dev/null && echo "---" && redis-cli info stats 2>/dev/null | head -15`
		case "es":
			script = `echo "=== Elasticsearch Analysis ===" && curl -s http://localhost:9200/_nodes/stats/jvm 2>/dev/null | head -20`
		default:
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown app: " + req.App})
			return
		}
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "os_app_tuning_analyze")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "analyze"})

	case "recommend":
		var script string
		switch req.App {
		case "jvm":
			script = `echo "=== JVM Recommendations ===" && echo "1. -Xms/-Xmx: 75% of available RAM" && echo "2. -XX:+UseG1GC (G1 garbage collector)" && echo "3. -XX:MaxGCPauseMillis=200" && echo "4. -XX:+HeapDumpOnOutOfMemoryError" && echo "5. -XX:+UseStringDeduplication"`
		case "mysql":
			script = `echo "=== MySQL Recommendations ===" && echo "1. innodb_buffer_pool_size: 70% of RAM" && echo "2. innodb_log_file_size: 256M-1G" && echo "3. max_connections: 500-1000" && echo "4. innodb_flush_log_at_trx_commit: 2 (performance) or 1 (durability)" && echo "5. query_cache_size: 0 (disabled in 8.0+)" && echo "6. slow_query_log: ON"`
		case "nginx":
			script = `echo "=== Nginx Recommendations ===" && echo "1. worker_processes: auto" && echo "2. worker_connections: 4096-8192" && echo "3. keepalive_timeout: 65" && echo "4. gzip: on, gzip_types text/plain text/css application/json" && echo "5. client_max_body_size: 50m" && echo "6. proxy_buffer_size: 128k"`
		case "redis":
			script = `echo "=== Redis Recommendations ===" && echo "1. maxmemory: 75% of RAM" && echo "2. maxmemory-policy: allkeys-lru" && echo "3. tcp-backlog: 511" && echo "4. timeout: 300" && echo "5. appendonly: yes (AOF)" && echo "6. save: 900 1 300 10 60 10000" && echo "7. io-threads: 4-8"`
		case "es":
			script = `echo "=== Elasticsearch Recommendations ===" && echo "1. heap size: 50% of RAM, max 31GB" && echo "2. bootstrap.memory_lock: true" && echo "3. indices.memory.index_buffer_size: 30%" && echo "4. thread_pool.search.size: 2x cores" && echo "5. discovery.zen.minimum_master_nodes: (N/2)+1" && echo "6. shard size: 20-50GB per shard"`
		default:
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown app: " + req.App})
			return
		}
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "os_app_tuning_recommend")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "recommend"})

	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
	}
}

// ============================================================================
// 存储调优 (I/O scheduler, filesystem)
// ============================================================================

// handleStorageTuning 处理 POST /api/v1/os/storage-tuning
func (s *Server) handleStorageTuning(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // get/set/analyze
		Device   string `json:"device"` // /dev/sda, /dev/nvme0n1
		Sched    string `json:"sched"`  // deadline/none/mq-deadline/kyber/bfq
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	switch req.Action {
	case "get":
		script := `echo "=== I/O Schedulers ===" && for d in /sys/block/*/queue/scheduler; do echo "$d: $(cat $d)"; done && echo "=== Read Ahead ===" && for d in /sys/block/*/queue/read_ahead_kb; do echo "$d: $(cat $d)"; done`
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "os_storage_tuning_get")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "get"})

	case "set":
		if req.Device == "" || req.Sched == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "device and sched are required"})
			return
		}
		script := fmt.Sprintf(`echo %s > /sys/block/%s/queue/scheduler 2>/dev/null && echo "Scheduler set to %s for %s" || echo "Failed"`, req.Sched, req.Device, req.Sched, req.Device)
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "os_storage_tuning_set")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "set"})

	case "analyze":
		script := `echo "=== Disk I/O Analysis ===" && iostat -x 1 3 2>/dev/null | tail -20 || echo "iostat not available" && echo "=== Disk Usage ===" && df -h && echo "=== Inodes ===" && df -i`
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "os_storage_tuning_analyze")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "analyze"})

	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
	}
}

// ============================================================================
// 证书管理 (SSL/TLS 证书)
// ============================================================================

// handleOSCert 处理 POST /api/v1/os/cert
func (s *Server) handleOSCert(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // list/generate/renew/check
		CertPath string `json:"certPath"`
		Days     int    `json:"days"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	switch req.Action {
	case "list":
		script := `find /etc/ssl/certs /etc/pki/tls/certs -name "*.crt" -o -name "*.pem" 2>/dev/null | head -20`
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "os_cert_list")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "list"})

	case "check":
		if req.CertPath == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "certPath is required"})
			return
		}
		script := fmt.Sprintf(`openssl x509 -in %s -noout -subject -issuer -dates 2>/dev/null || echo "Certificate not found"`, req.CertPath)
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "os_cert_check")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "check"})

	case "generate":
		days := req.Days
		if days <= 0 {
			days = 365
		}
		script := fmt.Sprintf(`openssl req -x509 -nodes -days %d -newkey rsa:2048 -keyout /tmp/server.key -out /tmp/server.crt -subj "/CN=localhost" 2>/dev/null && echo "Certificate generated: /tmp/server.crt (valid %d days)"`, days, days)
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "os_cert_generate")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "generate"})

	case "renew":
		if req.CertPath == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "certPath is required"})
			return
		}
		script := fmt.Sprintf(`echo "Renewing certificate: %s (manual steps required)"`, req.CertPath)
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "os_cert_renew")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "renew"})

	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
	}
}

// ============================================================================
// 资源管理 (cgroups, ulimit, quotas)
// ============================================================================

// handleOSResource 处理 POST /api/v1/os/resource
func (s *Server) handleOSResource(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // cgroups/ulimit/quota/list
		User     string `json:"user"`
		Limit    string `json:"limit"` // e.g. "nofile=65535", "nproc=4096"
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	switch req.Action {
	case "cgroups":
		script := `echo "=== Cgroups v1 ===" && cat /proc/cgroups 2>/dev/null && echo "=== Cgroups v2 ===" && ls /sys/fs/cgroup/ 2>/dev/null | head -20 && echo "=== Cgroup Controllers ===" && cat /sys/fs/cgroup/cgroup.controllers 2>/dev/null`
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "os_resource_cgroups")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "cgroups"})

	case "ulimit":
		if req.Limit == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "limit is required (e.g. nofile=65535)"})
			return
		}
		script := fmt.Sprintf(`ulimit -n %s 2>/dev/null && echo "ulimit set: %s" || echo "Failed to set ulimit"`, req.Limit, req.Limit)
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "os_resource_ulimit")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "ulimit"})

	case "quota":
		if req.User == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "user is required"})
			return
		}
		script := fmt.Sprintf(`quota -u %s 2>/dev/null || echo "Quota not enabled for %s"`, req.User, req.User)
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "os_resource_quota")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "quota"})

	case "list":
		script := `echo "=== Ulimit ===" && ulimit -a 2>/dev/null && echo "=== Cgroups ===" && cat /proc/cgroups 2>/dev/null | head -10`
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "os_resource_list")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "list"})

	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
	}
}

// ============================================================================
// 系统备份/恢复
// ============================================================================

// handleOSSystemBackup 处理 POST /api/v1/os/system-backup
func (s *Server) handleOSSystemBackup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // backup/restore/list
		Path     string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	path := req.Path
	if path == "" {
		path = "/var/backups/system"
	}

	switch req.Action {
	case "backup":
		script := fmt.Sprintf(`mkdir -p %s && tar czf %s/system-backup-$(date +%%Y%%m%%d).tar.gz /etc /opt /var/lib 2>/dev/null && echo "System backup created: %s/system-backup-$(date +%%Y%%m%%d).tar.gz"`, path, path, path)
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "os_system_backup_backup")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "backup"})

	case "restore":
		script := fmt.Sprintf(`echo "Restoring system from %s (manual steps required)"`, path)
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "os_system_backup_restore")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "restore"})

	case "list":
		script := fmt.Sprintf(`ls -lh %s 2>/dev/null || echo "No backup files found"`, path)
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "os_system_backup_list")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "list"})

	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
	}
}

// ============================================================================
// 路由注册
// ============================================================================

// registerOSTuningRoutes 注册 OS 调优路由
func (s *Server) registerOSTuningRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/os/hardware/inventory-full", s.handleHardwareInventoryFull)
	mux.HandleFunc("/api/v1/os/hardware/sensors", s.handleHardwareSensors)
	mux.HandleFunc("/api/v1/os/kernel/tuning", s.handleKernelTuning)
	mux.HandleFunc("/api/v1/os/kernel/module", s.handleOSKernelModule)
	mux.HandleFunc("/api/v1/os/app-tuning", s.handleAppTuning)
	mux.HandleFunc("/api/v1/os/storage-tuning", s.handleStorageTuning)
	mux.HandleFunc("/api/v1/os/cert", s.handleOSCert)
	mux.HandleFunc("/api/v1/os/resource", s.handleOSResource)
	mux.HandleFunc("/api/v1/os/system-backup", s.handleOSSystemBackup)
}
