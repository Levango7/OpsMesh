-- 005_m2_alert_governance.sql — M2 告警治理：静默规则 / 通知渠道 / 通知模板 持久化
--
-- 任务 246：将 sql_m2.go 中 alertRules/silences/notifyChannels/notifyTemplates 的内存 map
-- 实现升级为 SQL 持久化，使 --store=mysql 时重启不丢失，多副本 HA 下数据一致。
--
-- 本迁移新增三张表（alert_silences / notify_channels / notify_templates），
-- 并为已有 alert_rules 表补 created_by 列（记录规则创建人，供 controlplane 迁移
-- globalAlertRules 到 store 接口时持久化 CreatedBy 字段）。
--
-- 兼容性：
--   - ALTER TABLE ... ADD COLUMN IF NOT EXISTS 需要 MySQL 8.0+；老版本由
--     applyLegacyColumnFixups 中 alterColumnIfMissing 兜底（sql.go 末尾追加）。
--   - CREATE TABLE IF NOT EXISTS 对已存在库幂等。
--   - 新增列均 NULL 或带默认值，老规则 created_by=NULL，不影响现有逻辑。
--
-- 表结构说明：
--   - alert_silences：基于标签匹配 + 时间窗口的批量静默规则（SilenceRule）。
--   - notify_channels：通知渠道（钉钉/企业微信/飞书/Slack/邮件/Webhook）配置。
--   - notify_templates：通知消息模板（Go text/template 变量替换）。
--   - JSON 列（matchers/config）存储结构化字段，与应用层 json.Marshal/Unmarshal 对齐。
--   - 所有表均按 tenant_id 隔离，索引 idx_tenant 加速按租户过滤。

-- alert_rules 补 created_by 列（记录规则创建人）。
ALTER TABLE alert_rules ADD COLUMN created_by VARCHAR(64);

-- alert_silences：静默规则表（基于标签匹配 + 时间窗口抑制告警事件）。
CREATE TABLE IF NOT EXISTS alert_silences (
    id VARCHAR(64) PRIMARY KEY,
    tenant_id VARCHAR(64) NOT NULL,
    match_labels JSON,
    starts_at DATETIME,
    ends_at DATETIME,
    created_by VARCHAR(64),
    reason TEXT,
    created_at DATETIME,
    INDEX idx_tenant (tenant_id)
);

-- notify_channels：通知渠道表（钉钉/企业微信/飞书/Slack/邮件/Webhook 配置）。
CREATE TABLE IF NOT EXISTS notify_channels (
    id VARCHAR(64) PRIMARY KEY,
    tenant_id VARCHAR(64) NOT NULL,
    name VARCHAR(255) NOT NULL,
    type VARCHAR(32) NOT NULL,
    config JSON,
    enabled TINYINT(1) DEFAULT 1,
    created_at DATETIME,
    updated_at DATETIME,
    INDEX idx_tenant (tenant_id)
);

-- notify_templates：通知模板表（消息标题/正文模板，Go text/template 变量替换）。
CREATE TABLE IF NOT EXISTS notify_templates (
    id VARCHAR(64) PRIMARY KEY,
    tenant_id VARCHAR(64) NOT NULL,
    name VARCHAR(255) NOT NULL,
    type VARCHAR(32) NOT NULL,
    title TEXT,
    body TEXT,
    format VARCHAR(16),
    created_at DATETIME,
    updated_at DATETIME,
    INDEX idx_tenant (tenant_id)
);