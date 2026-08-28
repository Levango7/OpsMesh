-- incident-svc schema.sql — MySQL schema for incident service
-- Tables: incidents, timeline_events

CREATE TABLE IF NOT EXISTS incidents (
    id VARCHAR(64) PRIMARY KEY,
    title VARCHAR(255) NOT NULL,
    description TEXT,
    severity VARCHAR(16),
    status VARCHAR(32) DEFAULT 'detected',
    alert_ids JSON,
    device_ids JSON,
    assignee VARCHAR(64),
    tags JSON,
    detected_at DATETIME,
    resolved_at DATETIME,
    closed_at DATETIME,
    created_at DATETIME,
    updated_at DATETIME,
    INDEX idx_status (status),
    INDEX idx_severity (severity)
);

CREATE TABLE IF NOT EXISTS timeline_events (
    id VARCHAR(64) PRIMARY KEY,
    incident_id VARCHAR(64) NOT NULL,
    timestamp DATETIME,
    type VARCHAR(64),
    description TEXT,
    author VARCHAR(64),
    INDEX idx_incident (incident_id)
);
