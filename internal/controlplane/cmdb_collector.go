// cmdb_collector.go — CMDB 采集自动化：控制面周期性从 agent 上报的设备指标采集
// 主机/服务元信息，更新 CMDB 配置项（CI）。
//
// 设计要点：
//   - 复用 agent 心跳上报的 DeviceMetrics（不额外发请求，零侵入采集）；
//   - 主机元信息（CPU 核数/内存总量/磁盘总量/OS/内核）写入 machine CI；
//   - 服务列表写入 service CI（一台设备多个服务 → 多个 service CI）；
//   - CI ID 稳定（ci-host-<deviceID> / ci-svc-<deviceID>-<svcName>），幂等更新；
//   - 仅 leader 执行 CollectAll（避免多副本重复写 CI）；
//   - 采集失败不阻断循环（单台设备失败记 failed，继续下一台）。
//
// 路由：POST /api/v1/cmdb/collect 手动触发全量采集（返回 {collected, failed}）。
package controlplane

import (
	"context"

	"opsmesh/internal/controlplane/paginate"

	"fmt"
	"net/http"
	"strconv"
	"time"

	"opsmesh/internal/cmdb"
	"opsmesh/internal/logx"
	"opsmesh/internal/otelx"
	"opsmesh/internal/proto"
	"opsmesh/internal/store"
)

// cmdbCollectDefaultInterval 默认采集间隔（5 分钟）。
const cmdbCollectDefaultInterval = 5 * time.Minute

// CMDBCollector CMDB 采集器：定时从 agent 指标采集主机/服务元信息，更新 CMDB。
//
// 复用 agent 心跳上报的 DeviceMetrics（store.DeviceMetrics），不额外向 agent 发请求；
// 周期遍历所有在线设备，提取主机元信息 + 服务列表，更新或创建对应 CMDB CI。
//
// 字段说明：
//   - store：设备/审计存储（读 DeviceMetrics、写 AuditEvent）；
//   - ciStore：CMDB CI 存储（CRUD CI）；
//   - interval：采集间隔（<=0 时回退 5 分钟）；
//   - tenantID：租户隔离（空=全部租户，多租户场景由调用方按租户构造多个 collector）。
type CMDBCollector struct {
	store    store.Store   // 设备指标 + 审计
	ciStore  cmdb.CiStore  // CMDB CI CRUD
	interval time.Duration // 采集间隔
	tenantID string        // 租户隔离
}

// NewCMDBCollector 构造 CMDB 采集器。
//
// 参数：
//   - st：设备/审计存储（读 DeviceMetrics、写 AuditEvent）；
//   - ci：CMDB CI 存储（CRUD CI）；
//   - interval：采集间隔（<=0 时回退 5 分钟）；
//   - tenantID：租户隔离（空=全部租户）。
func NewCMDBCollector(st store.Store, ci cmdb.CiStore, interval time.Duration, tenantID string) *CMDBCollector {
	if interval <= 0 {
		interval = cmdbCollectDefaultInterval
	}
	return &CMDBCollector{
		store:    st,
		ciStore:  ci,
		interval: interval,
		tenantID: tenantID,
	}
}

// Collect 从单个设备的最新指标采集 CMDB 信息。
//
// 提取 CPU 核数/内存总量/磁盘总量/OS 类型/OS 版本/内核版本/服务列表，
// 更新或创建对应的 CMDB 配置项（machine CI + service CI）。
//
// 逻辑：
//  1. 从 metrics 提取主机元信息（CPU/Memory/Disk/OS/Hostname）；
//  2. 构造 machine CI（类型 "machine"，属性 hostname/os_type/os_version/kernel/cpu_cores/memory_total/disk_total）；
//  3. 对 metrics.Services 中每个服务构造 service CI（类型 "service"，属性 name/status/enabled）；
//  4. 调用 ciStore 的 GetCI/CreateCI/UpdateCI 更新或创建；
//  5. 记录审计日志（action=cmdb_collect）。
//
// deviceID 为空或 metrics 为 nil 时直接返回（不报错，幂等）。
func (c *CMDBCollector) Collect(deviceID string, metrics *proto.DeviceMetrics) error {
	if deviceID == "" || metrics == nil {
		return nil
	}
	ctx := context.Background()
	now := time.Now()

	// 1. 构造主机 CI（machine 类型，对应内置 CI 类型字典）。
	hostCIID := fmt.Sprintf("ci-host-%s", deviceID)
	hostname := metrics.Hostname
	if hostname == "" {
		hostname = deviceID // 回退用 deviceID 作展示名
	}
	diskTotal := uint64(0)
	for _, d := range metrics.Disks {
		diskTotal += d.Total
	}
	hostAttrs := map[string]string{
		"hostname":     hostname,
		"os_type":      metrics.OS,
		"os_version":   metrics.OSVersion,
		"kernel":       metrics.Kernel,
		"arch":         metrics.Arch,
		"cpu_cores":    strconv.Itoa(metrics.CPU.Cores),
		"cpu_model":    metrics.CPU.Model,
		"memory_total": strconv.FormatUint(metrics.Memory.Total, 10),
		"disk_total":   strconv.FormatUint(diskTotal, 10),
	}

	if err := c.upsertCI(ctx, hostCIID, "machine", hostname, deviceID, hostAttrs, now); err != nil {
		return fmt.Errorf("upsert host CI: %w", err)
	}

	// 2. 构造服务 CI（service 类型，一台设备多个服务 → 多个 CI）。
	for _, svc := range metrics.Services {
		svcCIID := fmt.Sprintf("ci-svc-%s-%s", deviceID, svc.Name)
		svcAttrs := map[string]string{
			"name":    svc.Name,
			"status":  svc.Status,
			"enabled": strconv.FormatBool(svc.Enabled),
			"host":    hostname,
		}
		if err := c.upsertCI(ctx, svcCIID, "service", svc.Name, deviceID, svcAttrs, now); err != nil {
			// 单个服务 CI 失败不阻断整体采集，记日志继续。
			logx.Warn(ctx, "CMDB 采集: 服务 CI 更新失败", err, "deviceID", deviceID, "service", svc.Name)
		}
	}

	// 3. 记录审计日志（等保三级留痕）。
	// hostname 为 agent 可控字符串，Detail 须经 sanitizeAuditDetail 脱敏
	// （与全项目审计口径一致）；直调 store.Audit 时需自行补 TraceID。
	c.store.Audit(&proto.AuditEvent{
		TenantID:  c.tenantID,
		Action:    "cmdb_collect",
		Target:    deviceID,
		Detail:    sanitizeAuditDetail(fmt.Sprintf("host=%s services=%d", hostname, len(metrics.Services))),
		TraceID:   otelx.TraceIDFromContext(ctx),
		CreatedAt: now,
	})
	return nil
}

// upsertCI 按 ID 幂等更新或创建 CI。
//
// 已存在则 UpdateCI（产生新版本）；不存在则 CreateCI。
// tenantID/agentID/deviceID/source 由 collector 统一填充，attrs 为采集到的属性集。
func (c *CMDBCollector) upsertCI(ctx context.Context, id, ciType, name, deviceID string, attrs map[string]string, now time.Time) error {
	tenantID := c.tenantID
	if tenantID == "" {
		tenantID = "default"
	}
	existing, err := c.ciStore.GetCI(ctx, id, tenantID)
	if err == nil && existing != nil {
		// 更新：深拷贝属性 map（避免修改 store 内部 map 引发并发写）。
		// existing.Attrs 是 store 返回的 map 引用，直接原地修改会在并发采集同设备时触发
		// concurrent map writes；深拷贝后各 goroutine 持有独立 map，UpdateCI 在 store mutex 下写入。
		merged := make(map[string]string)
		for k, v := range existing.Attrs {
			merged[k] = v
		}
		for k, v := range attrs {
			merged[k] = v
		}
		existing.Attrs = merged
		existing.Name = name
		existing.UpdatedAt = now
		return c.ciStore.UpdateCI(ctx, existing)
	}
	// 创建新 CI。
	ci := &cmdb.CiItem{
		ID:             id,
		CiType:         ciType,
		TenantID:       tenantID,
		Name:           name,
		Status:         "active",
		ApprovalStatus: cmdb.ApprovalApproved, // agent 采集自动入库，默认已审批
		Attrs:          attrs,
		Source:         "agent",
		DeviceID:       deviceID,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	return c.ciStore.CreateCI(ctx, ci)
}

// CollectAll 遍历所有在线设备，执行采集。
//
// 取所有设备快照（store.Snapshot），对每台非 retired 设备读取最新 DeviceMetrics，
// 调用 Collect 更新 CMDB。返回 (collected, failed, err)：
//   - collected：成功采集的设备数；
//   - failed：采集失败的设备数（单台失败不阻断整体）；
//   - err：仅当快照获取失败时返回（致命错误）。
//
// 无指标的设备跳过（agent 未上报过指标，可能是刚注册尚未到首个采集周期），不计 failed。
func (c *CMDBCollector) CollectAll() (collected, failed int, err error) {
	snap := c.store.Snapshot(c.tenantID)
	for _, devices := range snap {
		for _, d := range devices {
			if d.Retired {
				continue
			}
			metrics := c.store.DeviceMetrics(d.DeviceID)
			if metrics == nil {
				// 无指标：跳过（不报错，不计数）。
				continue
			}
			if e := c.Collect(d.DeviceID, metrics); e != nil {
				failed++
				logx.Warn(context.Background(), "CMDB 采集失败", e, "deviceID", d.DeviceID)
				continue
			}
			collected++
		}
	}
	return collected, failed, nil
}

// Run 启动定时采集循环（收到 ctx 取消时退出）。
//
// 每 interval 周期调用 CollectAll；仅 leader 执行采集（避免多副本重复写 CI）。
// interval<=0 时回退 5 分钟。首次启动立即执行一次（让重启后 CMDB 快速同步）。
func (c *CMDBCollector) Run(ctx context.Context) {
	interval := c.interval
	if interval <= 0 {
		interval = cmdbCollectDefaultInterval
	}
	// 首次立即执行一次（重启后快速同步 CMDB）。
	c.collectOnceIfLeader(ctx)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.collectOnceIfLeader(ctx)
		}
	}
}

// collectOnceIfLeader 仅 leader 执行一次全量采集。
//
// 非 leader 跳过（避免多副本重复写 CI）；MemoryStore 恒为 leader（单实例）。
func (c *CMDBCollector) collectOnceIfLeader(ctx context.Context) {
	if !c.store.IsLeader() {
		return
	}
	collected, failed, err := c.CollectAll()
	if err != nil {
		logx.Warn(ctx, "CMDB 定时采集失败", err)
		return
	}
	if collected > 0 || failed > 0 {
		logx.Info(ctx, "CMDB 定时采集完成", "collected", collected, "failed", failed)
	}
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
