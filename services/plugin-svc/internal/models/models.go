package models

import "time"

// PluginStatus represents the lifecycle state of a plugin.
type PluginStatus string

const (
	StatusPending      PluginStatus = "pending"
	StatusInstalled    PluginStatus = "installed"
	StatusActive       PluginStatus = "active"
	StatusIncompatible PluginStatus = "incompatible"
)

// PluginType represents the category of a plugin.
type PluginType string

const (
	PluginTypeData        PluginType = "data"
	PluginTypeLogic       PluginType = "logic"
	PluginTypeIntegration PluginType = "integration"
)

// ValidPluginTypes is the whitelist of allowed plugin types.
var ValidPluginTypes = map[PluginType]bool{
	PluginTypeData:        true,
	PluginTypeLogic:       true,
	PluginTypeIntegration: true,
}

// Plugin represents a plugin entity in the marketplace.
type Plugin struct {
	ID          string       `json:"id"`
	Name        string       `json:"name"`
	Version     string       `json:"version"`
	Description string       `json:"description"`
	Author      string       `json:"author"`
	Type        PluginType   `json:"type"`
	Category    string       `json:"category"`
	Tags        []string     `json:"tags"`
	DownloadURL string       `json:"downloadURL"`
	Checksum    string       `json:"checksum"`
	Status      PluginStatus `json:"status"`
	Installed   bool         `json:"installed"`
	Enabled     bool         `json:"enabled"`
	CreatedAt   time.Time    `json:"createdAt"`
	UpdatedAt   time.Time    `json:"updatedAt"`
}

// PluginVersion represents a version entry in the plugin's version history.
type PluginVersion struct {
	PluginID    string    `json:"pluginId"`
	Version     string    `json:"version"`
	Checksum    string    `json:"checksum"`
	DownloadURL string    `json:"downloadURL"`
	ReleasedAt  time.Time `json:"releasedAt"`
	Changelog   string    `json:"changelog"`
}

// SearchQuery represents search/filter parameters for plugins.
type SearchQuery struct {
	Name     string `json:"name"`
	Tag      string `json:"tag"`
	Category string `json:"category"`
	Type     string `json:"type"`
	Status   string `json:"status"`
}
