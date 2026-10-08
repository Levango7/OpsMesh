// idgen.go 领域实体 ID 生成 helper（TD-61，自 internal/store/memory_*.go 上提）。
//
// 为什么在中性层：内存与 SQL 两个后端共用同一套 ID 形态（前缀 + 16 字节 crypto/rand hex，
// 熵源失败回退时间戳）；此前定义在 memory_*.go、由 sql_*.go 反向引用。
//
// 命名：原名 randXxxID 导出为 RandXxxID。
package model

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

// RandAPIKeyID 生成随机 API Key ID（"apikey-" + 16 字节 hex）。
func RandAPIKeyID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("apikey-%d", time.Now().UnixNano())
	}
	return "apikey-" + hex.EncodeToString(b)
}

// RandArgoCDID 生成随机 ArgoCD 应用 ID（"argocd-" + 16 字节 hex）。
func RandArgoCDID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("argocd-%d", time.Now().UnixNano())
	}
	return "argocd-" + hex.EncodeToString(b)
}

// RandAutomationExecID 生成随机自动化执行记录 ID（"exec-" + 16 字节 hex）。
func RandAutomationExecID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("exec-%d", time.Now().UnixNano())
	}
	return "exec-" + hex.EncodeToString(b)
}

// RandAutomationRuleID 生成随机自动化规则 ID（"rule-" + 16 字节 hex）。
func RandAutomationRuleID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("rule-%d", time.Now().UnixNano())
	}
	return "rule-" + hex.EncodeToString(b)
}

// RandBackupID 生成随机备份记录 ID（"backup-" + 16 字节 hex）。
func RandBackupID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("backup-%d", time.Now().UnixNano())
	}
	return "backup-" + hex.EncodeToString(b)
}

// RandBillingID 生成随机计费 ID（prefix + 16 字节 hex）。
func RandBillingID(prefix string) string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
	}
	return prefix + "-" + hex.EncodeToString(b)
}

// RandComplianceID 生成随机合规报告 ID（"compliance-" + 16 字节 hex）。
func RandComplianceID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("compliance-%d", time.Now().UnixNano())
	}
	return "compliance-" + hex.EncodeToString(b)
}

// RandK8sClusterID 生成随机 K8s 集群 ID（16 字节十六进制，crypto/rand 密码学安全）。
// 用于 SaveK8sCluster 分配 ID（调用方未填 ID 时）。
func RandK8sClusterID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// 熵源失败回退时间戳（降级但可容忍，唯一性由 k8sClusters map key 兜底）。
		return fmt.Sprintf("k8s-cluster-%d", time.Now().UnixNano())
	}
	return "k8s-cluster-" + hex.EncodeToString(b)
}

func RandMiddlewareTemplateID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("mw-tmpl-%d", time.Now().UnixNano())
	}
	return "mw-tmpl-" + hex.EncodeToString(b)
}

// RandNetworkDeviceID 生成随机网络设备 ID（"netdev-" + 16 字节 hex）。
func RandNetworkDeviceID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("netdev-%d", time.Now().UnixNano())
	}
	return "netdev-" + hex.EncodeToString(b)
}

// RandNotifyChannelID 生成随机通知渠道 ID。
func RandNotifyChannelID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("ch-%d", time.Now().UnixNano())
	}
	return "ch-" + hex.EncodeToString(b)
}

// RandNotifyTemplateID 生成随机通知模板 ID。
func RandNotifyTemplateID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("tpl-%d", time.Now().UnixNano())
	}
	return "tpl-" + hex.EncodeToString(b)
}

// RandOSTemplateID 生成随机 OS 安装模板 ID（16 字节十六进制，crypto/rand 密码学安全）。
func RandOSTemplateID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("os-tmpl-%d", time.Now().UnixNano())
	}
	return "os-tmpl-" + hex.EncodeToString(b)
}

// RandPipelineID 生成随机流水线模板 ID（"pipeline-" + 16 字节 hex）。
func RandPipelineID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("pipeline-%d", time.Now().UnixNano())
	}
	return "pipeline-" + hex.EncodeToString(b)
}

// RandPluginID 生成随机插件 ID（"plugin-" + 16 字节 hex）。
func RandPluginID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("plugin-%d", time.Now().UnixNano())
	}
	return "plugin-" + hex.EncodeToString(b)
}

// RandRoleID 生成随机角色 ID。
func RandRoleID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("role-%d", time.Now().UnixNano())
	}
	return "role-" + hex.EncodeToString(b)
}

// RandRunID 生成随机运行记录 ID（"run-" + 16 字节 hex）。
func RandRunID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("run-%d", time.Now().UnixNano())
	}
	return "run-" + hex.EncodeToString(b)
}

// RandScriptExecutionID 生成随机脚本执行记录 ID（"script-exec-" + 16 字节 hex）。
func RandScriptExecutionID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("script-exec-%d", time.Now().UnixNano())
	}
	return "script-exec-" + hex.EncodeToString(b)
}

// RandScriptID 生成随机脚本 ID（"script-" + 16 字节 hex）。
func RandScriptID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("script-%d", time.Now().UnixNano())
	}
	return "script-" + hex.EncodeToString(b)
}

// RandSilenceID 生成随机静默规则 ID（16 字节十六进制，crypto/rand 密码学安全）。
func RandSilenceID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("silence-%d", time.Now().UnixNano())
	}
	return "silence-" + hex.EncodeToString(b)
}

// RandSLOID 生成随机 SLO ID（"slo-" + 16 字节 hex，crypto/rand 密码学安全）。
// 用于 CreateSLO 分配 ID（调用方未填 ID 时）。
func RandSLOID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// 熵源失败回退时间戳（降级但可容忍，唯一性由 slos map key 兜底）。
		return fmt.Sprintf("slo-%d", time.Now().UnixNano())
	}
	return "slo-" + hex.EncodeToString(b)
}

// RandTenantID 生成随机租户 ID（"tenant-" + 16 字节 hex）。
func RandTenantID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("tenant-%d", time.Now().UnixNano())
	}
	return "tenant-" + hex.EncodeToString(b)
}

// RandTicketID 生成随机工单 ID（"ticket-" + 16 字节 hex，crypto/rand 密码学安全）。
// 用于 CreateTicket 分配 ID（调用方未填 ID 时）。
func RandTicketID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// 熵源失败回退时间戳（降级但可容忍，唯一性由 tickets map key 兜底）。
		return fmt.Sprintf("ticket-%d", time.Now().UnixNano())
	}
	return "ticket-" + hex.EncodeToString(b)
}

// RandTrafficID 生成随机流量策略 ID（"traffic-" + 16 字节 hex）。
func RandTrafficID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("traffic-%d", time.Now().UnixNano())
	}
	return "traffic-" + hex.EncodeToString(b)
}

// RandUserID 生成随机用户 ID（16 字节十六进制，crypto/rand 密码学安全）。
// 用于 CreateUser 分配 ID（调用方未填 ID 时）。
func RandUserID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// 熵源失败回退时间戳（降级但可容忍，用户 ID 唯一性由 usersByName 索引兜底）。
		return fmt.Sprintf("user-%d", time.Now().UnixNano())
	}
	return "user-" + hex.EncodeToString(b)
}

// RandWebhookDeliveryID 生成随机投递记录 ID（"wh-delivery-" + 16 字节 hex）。
func RandWebhookDeliveryID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("wh-delivery-%d", time.Now().UnixNano())
	}
	return "wh-delivery-" + hex.EncodeToString(b)
}

// RandWebhookID 生成随机 Webhook ID（"webhook-" + 16 字节 hex）。
func RandWebhookID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("webhook-%d", time.Now().UnixNano())
	}
	return "webhook-" + hex.EncodeToString(b)
}
