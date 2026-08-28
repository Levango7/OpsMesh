package service

import (
	"strings"
	"testing"

	"github.com/Levango7/OpsMesh/services/plugin-svc/internal/models"
	"github.com/Levango7/OpsMesh/services/plugin-svc/internal/store"
)

func newTestService() *Service {
	st := store.NewMemoryStore()
	return NewService(st)
}

func TestCreatePlugin(t *testing.T) {
	svc := newTestService()
	p := &models.Plugin{
		Name:     "Test Plugin",
		Version:  "1.0.0",
		Type:     models.PluginTypeData,
		Category: "monitoring",
		Tags:     []string{"test", "metrics"},
	}
	created, err := svc.CreatePlugin(p)
	if err != nil {
		t.Fatalf("CreatePlugin failed: %v", err)
	}
	if created.ID == "" {
		t.Error("expected plugin ID to be set")
	}
	if created.Status != models.StatusPending {
		t.Errorf("expected status %s, got %s", models.StatusPending, created.Status)
	}
	if created.Installed {
		t.Error("expected plugin to not be installed initially")
	}
}

func TestCreatePluginMissingName(t *testing.T) {
	svc := newTestService()
	_, err := svc.CreatePlugin(&models.Plugin{Version: "1.0.0", Type: models.PluginTypeData})
	if err == nil {
		t.Fatal("expected error for missing name")
	}
}

func TestCreatePluginMissingVersion(t *testing.T) {
	svc := newTestService()
	_, err := svc.CreatePlugin(&models.Plugin{Name: "Test", Type: models.PluginTypeData})
	if err == nil {
		t.Fatal("expected error for missing version")
	}
}

func TestCreatePluginInvalidType(t *testing.T) {
	svc := newTestService()
	_, err := svc.CreatePlugin(&models.Plugin{Name: "Test", Version: "1.0.0", Type: "invalid"})
	if err == nil {
		t.Fatal("expected error for invalid type")
	}
}

func TestCreatePluginSSRFBlocked(t *testing.T) {
	svc := newTestService()
	_, err := svc.CreatePlugin(&models.Plugin{
		Name:        "Test",
		Version:     "1.0.0",
		Type:        models.PluginTypeData,
		DownloadURL: "http://192.168.1.1/plugin.bin",
	})
	if err != ErrSSRFBlocked {
		t.Fatalf("expected ErrSSRFBlocked, got: %v", err)
	}
}

func TestGetPlugin(t *testing.T) {
	svc := newTestService()
	created, _ := svc.CreatePlugin(&models.Plugin{Name: "Test", Version: "1.0.0", Type: models.PluginTypeData})
	got, err := svc.GetPlugin(created.ID)
	if err != nil {
		t.Fatalf("GetPlugin failed: %v", err)
	}
	if got.Name != "Test" {
		t.Errorf("expected name Test, got %s", got.Name)
	}
}

func TestGetPluginNotFound(t *testing.T) {
	svc := newTestService()
	_, err := svc.GetPlugin("nonexistent")
	if err != ErrPluginNotFound {
		t.Fatalf("expected ErrPluginNotFound, got: %v", err)
	}
}

func TestListPlugins(t *testing.T) {
	svc := newTestService()
	for i := 0; i < 3; i++ {
		_, err := svc.CreatePlugin(&models.Plugin{
			Name:    "Plugin",
			Version: "1.0.0",
			Type:    models.PluginTypeData,
		})
		if err != nil {
			t.Fatalf("CreatePlugin failed: %v", err)
		}
	}
	plugins := svc.ListPlugins()
	if len(plugins) != 3 {
		t.Errorf("expected 3 plugins, got %d", len(plugins))
	}
}

func TestUpdatePlugin(t *testing.T) {
	svc := newTestService()
	created, _ := svc.CreatePlugin(&models.Plugin{Name: "Original", Version: "1.0.0", Type: models.PluginTypeData})
	created.Name = "Updated"
	updated, err := svc.UpdatePlugin(created)
	if err != nil {
		t.Fatalf("UpdatePlugin failed: %v", err)
	}
	if updated.Name != "Updated" {
		t.Errorf("expected name Updated, got %s", updated.Name)
	}
}

func TestDeletePlugin(t *testing.T) {
	svc := newTestService()
	created, _ := svc.CreatePlugin(&models.Plugin{Name: "ToDelete", Version: "1.0.0", Type: models.PluginTypeData})
	err := svc.DeletePlugin(created.ID)
	if err != nil {
		t.Fatalf("DeletePlugin failed: %v", err)
	}
	_, err = svc.GetPlugin(created.ID)
	if err != ErrPluginNotFound {
		t.Fatalf("expected ErrPluginNotFound after delete, got: %v", err)
	}
}

func TestInstallPlugin(t *testing.T) {
	svc := newTestService()
	created, _ := svc.CreatePlugin(&models.Plugin{Name: "Test", Version: "1.0.0", Type: models.PluginTypeData})
	installed, err := svc.InstallPlugin(created.ID)
	if err != nil {
		t.Fatalf("InstallPlugin failed: %v", err)
	}
	if !installed.Installed {
		t.Error("expected plugin to be installed")
	}
	if installed.Status != models.StatusInstalled {
		t.Errorf("expected status %s, got %s", models.StatusInstalled, installed.Status)
	}
}

func TestInstallPluginNotFound(t *testing.T) {
	svc := newTestService()
	_, err := svc.InstallPlugin("nonexistent")
	if err != ErrPluginNotFound {
		t.Fatalf("expected ErrPluginNotFound, got: %v", err)
	}
}

func TestUninstallPlugin(t *testing.T) {
	svc := newTestService()
	created, _ := svc.CreatePlugin(&models.Plugin{Name: "Test", Version: "1.0.0", Type: models.PluginTypeData})
	svc.InstallPlugin(created.ID)
	uninstalled, err := svc.UninstallPlugin(created.ID)
	if err != nil {
		t.Fatalf("UninstallPlugin failed: %v", err)
	}
	if uninstalled.Installed {
		t.Error("expected plugin to be uninstalled")
	}
	if uninstalled.Status != models.StatusPending {
		t.Errorf("expected status %s, got %s", models.StatusPending, uninstalled.Status)
	}
}

func TestUpgradePlugin(t *testing.T) {
	svc := newTestService()
	created, _ := svc.CreatePlugin(&models.Plugin{Name: "Test", Version: "1.0.0", Type: models.PluginTypeData})
	svc.InstallPlugin(created.ID)
	upgraded, err := svc.UpgradePlugin(created.ID, "2.0.0", "abc123", "https://example.com/plugin-v2.bin")
	if err != nil {
		t.Fatalf("UpgradePlugin failed: %v", err)
	}
	if upgraded.Version != "2.0.0" {
		t.Errorf("expected version 2.0.0, got %s", upgraded.Version)
	}
}

func TestUpgradePluginNotInstalled(t *testing.T) {
	svc := newTestService()
	created, _ := svc.CreatePlugin(&models.Plugin{Name: "Test", Version: "1.0.0", Type: models.PluginTypeData})
	_, err := svc.UpgradePlugin(created.ID, "2.0.0", "", "")
	if err != ErrPluginNotInstalled {
		t.Fatalf("expected ErrPluginNotInstalled, got: %v", err)
	}
}

func TestGetVersions(t *testing.T) {
	svc := newTestService()
	created, _ := svc.CreatePlugin(&models.Plugin{Name: "Test", Version: "1.0.0", Type: models.PluginTypeData})
	svc.InstallPlugin(created.ID)
	svc.UpgradePlugin(created.ID, "2.0.0", "", "")
	versions, err := svc.GetVersions(created.ID)
	if err != nil {
		t.Fatalf("GetVersions failed: %v", err)
	}
	if len(versions) < 2 {
		t.Errorf("expected at least 2 versions, got %d", len(versions))
	}
}

func TestSearchPlugins(t *testing.T) {
	svc := newTestService()
	svc.CreatePlugin(&models.Plugin{Name: "CPU Monitor", Version: "1.0.0", Type: models.PluginTypeData, Category: "monitoring", Tags: []string{"cpu", "metrics"}})
	svc.CreatePlugin(&models.Plugin{Name: "Log Parser", Version: "1.0.0", Type: models.PluginTypeLogic, Category: "logging", Tags: []string{"logs"}})
	svc.CreatePlugin(&models.Plugin{Name: "Slack Alert", Version: "1.0.0", Type: models.PluginTypeIntegration, Category: "alerting", Tags: []string{"slack"}})

	results := svc.SearchPlugins(models.SearchQuery{Name: "CPU"})
	if len(results) != 1 {
		t.Errorf("expected 1 result for name search, got %d", len(results))
	}

	results = svc.SearchPlugins(models.SearchQuery{Category: "logging"})
	if len(results) != 1 {
		t.Errorf("expected 1 result for category search, got %d", len(results))
	}

	results = svc.SearchPlugins(models.SearchQuery{Tag: "slack"})
	if len(results) != 1 {
		t.Errorf("expected 1 result for tag search, got %d", len(results))
	}

	results = svc.SearchPlugins(models.SearchQuery{Type: "data"})
	if len(results) != 1 {
		t.Errorf("expected 1 result for type search, got %d", len(results))
	}
}

func TestSearchPluginsNoMatch(t *testing.T) {
	svc := newTestService()
	svc.CreatePlugin(&models.Plugin{Name: "Test", Version: "1.0.0", Type: models.PluginTypeData})
	results := svc.SearchPlugins(models.SearchQuery{Name: "NonExistent"})
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}

func TestRollbackPlugin(t *testing.T) {
	svc := newTestService()
	created, _ := svc.CreatePlugin(&models.Plugin{Name: "Test", Version: "1.0.0", Type: models.PluginTypeData})
	svc.InstallPlugin(created.ID)
	svc.UpgradePlugin(created.ID, "2.0.0", "", "")
	rolledBack, err := svc.RollbackPlugin(created.ID, "1.0.0")
	if err != nil {
		t.Fatalf("RollbackPlugin failed: %v", err)
	}
	if rolledBack.Version != "1.0.0" {
		t.Errorf("expected version 1.0.0 after rollback, got %s", rolledBack.Version)
	}
}

func TestRollbackPluginVersionNotFound(t *testing.T) {
	svc := newTestService()
	created, _ := svc.CreatePlugin(&models.Plugin{Name: "Test", Version: "1.0.0", Type: models.PluginTypeData})
	svc.InstallPlugin(created.ID)
	_, err := svc.RollbackPlugin(created.ID, "9.9.9")
	if err != ErrVersionNotFound {
		t.Fatalf("expected ErrVersionNotFound, got: %v", err)
	}
}

func TestValidateDownloadURL(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		wantErr bool
		errMsg  string
	}{
		{"empty URL", "", false, ""},
		{"valid https", "https://example.com/plugin.bin", false, ""},
		{"valid http", "http://example.com/plugin.bin", false, ""},
		{"private IP", "http://192.168.1.1/plugin.bin", true, "blocked"},
		{"localhost", "http://localhost/plugin.bin", true, "blocked"},
		{"loopback", "http://127.0.0.1/plugin.bin", true, "blocked"},
		{"ftp scheme", "ftp://example.com/plugin.bin", true, "invalid scheme"},
		{"file scheme", "file:///etc/passwd", true, "invalid scheme"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateDownloadURL(tt.url)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateDownloadURL(%q) error = %v, wantErr %v", tt.url, err, tt.wantErr)
			}
			if err != nil && tt.errMsg != "" && !strings.Contains(err.Error(), tt.errMsg) {
				t.Errorf("validateDownloadURL(%q) error = %v, want containing %q", tt.url, err, tt.errMsg)
			}
		})
	}
}
