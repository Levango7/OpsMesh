-- 032_controlplane_instances.sql — 控制面实例心跳表（HA 多实例枚举）
--
-- 审计整改批次16：leader_lease 只记录当前 leader，无法枚举 follower。
-- 本表按实例粒度记录心跳，用于 HA 状态页展示「全部控制面实例 + 角色」。
-- 心跳周期默认 10s，TTL 30s（超过即视为下线，不参与展示）。

CREATE TABLE `controlplane_instances` (
    `instance_id` VARCHAR(128) NOT NULL COMMENT '实例唯一 ID（hostname+pid+纳秒）',
    `hostname` VARCHAR(255) NOT NULL DEFAULT '' COMMENT '主机名',
    `http_port` INT NOT NULL DEFAULT 0 COMMENT 'HTTP 端口',
    `grpc_port` INT NOT NULL DEFAULT 0 COMMENT 'gRPC 端口',
    `role` VARCHAR(16) NOT NULL DEFAULT 'follower' COMMENT '角色: leader/follower',
    `version` VARCHAR(64) NOT NULL DEFAULT '' COMMENT '构建版本',
    `started_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '进程启动时间',
    `last_heartbeat` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '最近心跳',
    `created_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (`instance_id`),
    KEY `idx_last_heartbeat` (`last_heartbeat`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='控制面实例心跳表';