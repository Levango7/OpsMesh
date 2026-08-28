-- autoscaler-svc schema.sql — MySQL schema for autoscaler service
-- Tables: scaling_rules, scaling_decisions

CREATE TABLE IF NOT EXISTS scaling_rules (
    id VARCHAR(64) PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    deployment VARCHAR(255),
    namespace VARCHAR(255),
    metric VARCHAR(128),
    scale_up_threshold DOUBLE,
    scale_down_threshold DOUBLE,
    min_replicas INT,
    max_replicas INT,
    cooldown_up BIGINT,
    cooldown_down BIGINT,
    enabled TINYINT(1) DEFAULT 1,
    created_at DATETIME,
    updated_at DATETIME,
    INDEX idx_deployment (deployment)
);

CREATE TABLE IF NOT EXISTS scaling_decisions (
    id VARCHAR(64) PRIMARY KEY,
    rule_id VARCHAR(64) NOT NULL,
    deployment VARCHAR(255),
    namespace VARCHAR(255),
    action VARCHAR(32),
    from_replicas INT,
    to_replicas INT,
    reason TEXT,
    metric_value DOUBLE,
    timestamp DATETIME,
    INDEX idx_rule (rule_id),
    INDEX idx_timestamp (timestamp)
);
