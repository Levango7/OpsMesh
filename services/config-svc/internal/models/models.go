package models

import "time"

// ConfigEntry represents a configuration item with versioning.
type ConfigEntry struct {
	ID          string
	TenantID    string
	Key         string
	Value       string
	Format      string // json/yaml/toml/properties/text
	Version     int
	Description string
	UpdatedBy   string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// SecretEntry represents a secret with plaintext value (internal only).
type SecretEntry struct {
	ID        string
	TenantID  string
	Key       string
	Value     string
	KeyType   string // aes/hmac/rsa/ecdsa/passphrase
	Version   int
	CreatedAt time.Time
	UpdatedAt time.Time
}

// SecretMeta represents secret metadata (external, no value).
type SecretMeta struct {
	ID        string
	TenantID  string
	Key       string
	KeyType   string
	Version   int
	CreatedAt time.Time
	UpdatedAt time.Time
}

// ChannelEntry represents a notification channel.
type ChannelEntry struct {
	ID        string
	TenantID  string
	Name      string
	Type      string // webhook/email/slack/dingtalk/wecom/feishu
	Config    string // JSON configuration
	Enabled   bool
	CreatedAt time.Time
	UpdatedAt time.Time
}

// TemplateEntry represents a configuration template.
type TemplateEntry struct {
	ID          string
	TenantID    string
	Name        string
	Description string
	Content     string            // Template content with {{variable}} placeholders
	Variables   map[string]string // Default variable values
	CreatedAt   time.Time
	UpdatedAt   time.Time
}
