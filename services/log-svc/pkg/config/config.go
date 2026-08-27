// Package config provides configuration structures for log-svc.
package config

import "time"

// Config is the top-level configuration for log-svc.
type Config struct {
	// Server configuration
	Server ServerConfig `json:"server" yaml:"server"`
	// LogStore backend configuration
	LogStore LogStoreConfig `json:"logstore" yaml:"logstore"`
	// Health check configuration
	Health HealthConfig `json:"health" yaml:"health"`
}

// ServerConfig configures the gRPC server.
type ServerConfig struct {
	// Address is the gRPC listen address (e.g., ":9090").
	Address string `json:"address" yaml:"address"`
	// MaxConcurrentStreams limits concurrent gRPC streams.
	MaxConcurrentStreams uint32 `json:"max_concurrent_streams" yaml:"max_concurrent_streams"`
	// MaxRecvMsgSize is the max message size in bytes (default 64MB).
	MaxRecvMsgSize int `json:"max_recv_msg_size" yaml:"max_recv_msg_size"`
}

// LogStoreConfig configures the log storage backend.
type LogStoreConfig struct {
	// Backend type: "memory", "sql", "loki", or "es".
	Backend string `json:"backend" yaml:"backend"`
	// Memory backend settings
	Memory MemoryConfig `json:"memory" yaml:"memory"`
	// SQL backend settings
	SQL SQLConfig `json:"sql" yaml:"sql"`
	// Loki backend settings
	Loki LokiConfig `json:"loki" yaml:"loki"`
	// Elasticsearch backend settings
	ES ESConfig `json:"es" yaml:"es"`
}

// MemoryConfig configures the in-memory backend.
type MemoryConfig struct {
	// Capacity is the max number of log entries to retain (default 5000).
	Capacity int `json:"capacity" yaml:"capacity"`
	// EnableIndex enables the inverted index for full-text search.
	EnableIndex bool `json:"enable_index" yaml:"enable_index"`
}

// SQLConfig configures the MySQL backend.
type SQLConfig struct {
	// DSN is the MySQL connection string.
	DSN string `json:"dsn" yaml:"dsn"`
	// MaxOpenConns limits open connections (default 25).
	MaxOpenConns int `json:"max_open_conns" yaml:"max_open_conns"`
	// MaxIdleConns limits idle connections (default 5).
	MaxIdleConns int `json:"max_idle_conns" yaml:"max_idle_conns"`
	// ConnMaxLifetime is the max connection lifetime.
	ConnMaxLifetime time.Duration `json:"conn_max_lifetime" yaml:"conn_max_lifetime"`
}

// LokiConfig configures the Loki backend.
type LokiConfig struct {
	// Endpoint is the Loki base URL (e.g., "http://loki:3100").
	Endpoint string `json:"endpoint" yaml:"endpoint"`
	// Timeout for Loki API requests (default 30s).
	Timeout time.Duration `json:"timeout" yaml:"timeout"`
	// LabelApp is the app label value (default "opsmesh").
	LabelApp string `json:"label_app" yaml:"label_app"`
}

// ESConfig configures the Elasticsearch backend.
type ESConfig struct {
	// Endpoint is the ES base URL (e.g., "http://es:9200").
	Endpoint string `json:"endpoint" yaml:"endpoint"`
	// Index is the ES index name (e.g., "opsmesh-logs").
	Index string `json:"index" yaml:"index"`
	// Timeout for ES API requests (default 30s).
	Timeout time.Duration `json:"timeout" yaml:"timeout"`
}

// HealthConfig configures the health check HTTP server.
type HealthConfig struct {
	// Address is the HTTP listen address (e.g., ":8080").
	Address string `json:"address" yaml:"address"`
}

// DefaultConfig returns a default configuration for development.
func DefaultConfig() *Config {
	return &Config{
		Server: ServerConfig{
			Address:              ":9090",
			MaxConcurrentStreams: 1000,
			MaxRecvMsgSize:       64 * 1024 * 1024, // 64MB
		},
		LogStore: LogStoreConfig{
			Backend: "memory",
			Memory: MemoryConfig{
				Capacity:    5000,
				EnableIndex: true,
			},
			SQL: SQLConfig{
				MaxOpenConns:    25,
				MaxIdleConns:    5,
				ConnMaxLifetime: 5 * time.Minute,
			},
			Loki: LokiConfig{
				Timeout:  30 * time.Second,
				LabelApp: "opsmesh",
			},
			ES: ESConfig{
				Timeout: 30 * time.Second,
				Index:   "opsmesh-logs",
			},
		},
		Health: HealthConfig{
			Address: ":8080",
		},
	}
}
