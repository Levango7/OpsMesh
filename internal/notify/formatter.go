package notify

import (
	"fmt"

	"opsmesh/internal/proto"
)

// feishuCard 构造飞书 interactive 卡片消息体（JSON serializable）。
// 文档：https://open.feishu.cn/document/uAjLw4CM/ukzMukzMukzM/feishu-cards/card-components
func feishuCard(a *proto.Alert) map[string]interface{} {
	return map[string]interface{}{
		"msg_type": "interactive",
		"card": map[string]interface{}{
			"header": map[string]interface{}{
				"title": map[string]interface{}{
					"tag":     "plain_text",
					"content": "🔴 OpsMesh 告警",
				},
				"template": "red",
			},
			"elements": []map[string]interface{}{
				{
					"tag": "markdown",
					"content": fmt.Sprintf(
						"**严重级别**: %s\n**设备**: %s\n**Agent**: %s\n**时间**: %s\n\n%s",
						a.Severity, a.DeviceID, a.AgentID,
						a.CreatedAt.Format("2006-01-02 15:04:05"),
						a.Message,
					),
				},
			},
		},
	}
}

// dingtalkMarkdown 构造钉钉 markdown 消息体。
// 文档：https://open.dingtalk.com/document/robots/custom-robot-access
func dingtalkMarkdown(a *proto.Alert) map[string]interface{} {
	return map[string]interface{}{
		"msgtype": "markdown",
		"markdown": map[string]interface{}{
			"title": "🔴 OpsMesh 告警",
			"text": fmt.Sprintf(
				"## 🔴 OpsMesh 告警\n\n"+
					"- **严重级别**: %s\n"+
					"- **设备**: %s\n"+
					"- **Agent**: %s\n"+
					"- **时间**: %s\n\n"+
					"%s",
				a.Severity, a.DeviceID, a.AgentID,
				a.CreatedAt.Format("2006-01-02 15:04:05"),
				a.Message,
			),
		},
	}
}

// PostByType 按通知类型推送告警。支持 generic / feishu / dingtalk。
// generic：直接 POST Alert JSON（默认，兼容现有逻辑）。
func PostByType(notifierType, webhookURL string, a *proto.Alert) error {
	if webhookURL == "" || a == nil {
		return nil
	}
	switch notifierType {
	case "feishu":
		return postJSON(webhookURL, feishuCard(a))
	case "dingtalk":
		return postJSON(webhookURL, dingtalkMarkdown(a))
	default: // generic
		return PostAlert(webhookURL, a)
	}
}
