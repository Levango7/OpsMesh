-- 037_governance.sql 数据治理平面持久化（DQ 规则/结果/PII/保留/目录）。
-- 与 internal/governance 的内存实现语义一致：所有查询按 tenant_id 分区。

CREATE TABLE IF NOT EXISTS gov_rules (
  tenant_id  VARCHAR(64)  NOT NULL,
  id         VARCHAR(64)  NOT NULL,
  dataset    VARCHAR(255) NOT NULL,
  col_name   VARCHAR(255) NOT NULL DEFAULT '',
  kind       VARCHAR(32)  NOT NULL,
  expr       TEXT         NULL,
  severity   VARCHAR(16)  NOT NULL DEFAULT 'warn',
  enabled    TINYINT(1)   NOT NULL DEFAULT 1,
  created_at DATETIME(6)  NOT NULL,
  updated_at DATETIME(6)  NOT NULL,
  PRIMARY KEY (tenant_id, id),
  KEY idx_gov_rules_tenant_created (tenant_id, created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS gov_rule_results (
  id         VARCHAR(64) NOT NULL,
  rule_id    VARCHAR(64) NOT NULL,
  tenant_id  VARCHAR(64) NOT NULL,
  passed     TINYINT(1)  NOT NULL,
  observed   TEXT        NULL,
  detail     TEXT        NULL,
  run_at     DATETIME(6) NOT NULL,
  PRIMARY KEY (id),
  KEY idx_gov_results_tenant_rule (tenant_id, rule_id, run_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS gov_pii_policies (
  tenant_id  VARCHAR(64)  NOT NULL,
  id         VARCHAR(64)  NOT NULL,
  dataset    VARCHAR(255) NOT NULL,
  col_name   VARCHAR(255) NOT NULL,
  kind       VARCHAR(32)  NOT NULL,
  mask       VARCHAR(255) NOT NULL DEFAULT '',
  enabled    TINYINT(1)   NOT NULL DEFAULT 1,
  created_at DATETIME(6)  NOT NULL,
  updated_at DATETIME(6)  NOT NULL,
  PRIMARY KEY (tenant_id, id),
  KEY idx_gov_pii_tenant_created (tenant_id, created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS gov_retention_policies (
  tenant_id      VARCHAR(64)  NOT NULL,
  id             VARCHAR(64)  NOT NULL,
  dataset        VARCHAR(255) NOT NULL,
  retention_days INT          NOT NULL,
  action         VARCHAR(32)  NOT NULL,
  enabled        TINYINT(1)   NOT NULL DEFAULT 1,
  created_at     DATETIME(6)  NOT NULL,
  updated_at     DATETIME(6)  NOT NULL,
  PRIMARY KEY (tenant_id, id),
  KEY idx_gov_retention_tenant_created (tenant_id, created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS gov_catalog_assets (
  tenant_id  VARCHAR(64)  NOT NULL,
  id         VARCHAR(64)  NOT NULL,
  kind       VARCHAR(32)  NOT NULL,
  name       VARCHAR(255) NOT NULL,
  owner      VARCHAR(255) NOT NULL DEFAULT '',
  tags_json  TEXT         NULL,
  pii_level  VARCHAR(16)  NOT NULL DEFAULT 'none',
  updated_at DATETIME(6)  NOT NULL,
  PRIMARY KEY (tenant_id, id),
  KEY idx_gov_catalog_tenant_updated (tenant_id, updated_at),
  KEY idx_gov_catalog_tenant_name (tenant_id, name)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
