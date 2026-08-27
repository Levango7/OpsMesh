package service

import (
	"context"
	"testing"

	configv1 "github.com/Levango7/OpsMesh/services/config-svc/api/proto/v1"
	"github.com/Levango7/OpsMesh/services/config-svc/internal/store"
)

func newTestService() *Service {
	st := store.NewMemoryStore("test-encryption-key", 50)
	return NewService(st)
}

// ===================== Config Tests =====================

func TestSetConfig(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	req := &configv1.SetConfigRequest{
		TenantId:    "tenant-1",
		Key:         "app/db/host",
		Value:       "localhost",
		Format:      "text",
		Description: "Database host",
		UpdatedBy:   "admin",
	}

	cfg, err := svc.SetConfig(ctx, req)
	if err != nil {
		t.Fatalf("SetConfig failed: %v", err)
	}

	if cfg.Id == "" {
		t.Error("expected config ID to be set")
	}
	if cfg.Value != "localhost" {
		t.Errorf("expected value 'localhost', got %s", cfg.Value)
	}
	if cfg.Version != 1 {
		t.Errorf("expected version 1, got %d", cfg.Version)
	}
	if cfg.CreatedAt == nil {
		t.Error("expected CreatedAt to be set")
	}
}

func TestSetConfigValidation(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	_, err := svc.SetConfig(ctx, &configv1.SetConfigRequest{
		TenantId: "",
		Key:      "test",
	})
	if err != ErrConfigInvalid {
		t.Fatalf("expected ErrConfigInvalid, got: %v", err)
	}

	_, err = svc.SetConfig(ctx, &configv1.SetConfigRequest{
		TenantId: "tenant-1",
		Key:      "",
	})
	if err != ErrConfigInvalid {
		t.Fatalf("expected ErrConfigInvalid, got: %v", err)
	}
}

func TestGetConfig(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	_, err := svc.SetConfig(ctx, &configv1.SetConfigRequest{
		TenantId: "tenant-1",
		Key:      "app/db/port",
		Value:    "5432",
	})
	if err != nil {
		t.Fatalf("SetConfig failed: %v", err)
	}

	cfg, err := svc.GetConfig(ctx, &configv1.GetConfigRequest{
		TenantId: "tenant-1",
		Key:      "app/db/port",
	})
	if err != nil {
		t.Fatalf("GetConfig failed: %v", err)
	}

	if cfg.Value != "5432" {
		t.Errorf("expected value '5432', got %s", cfg.Value)
	}
}

func TestGetConfigNotFound(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	_, err := svc.GetConfig(ctx, &configv1.GetConfigRequest{
		TenantId: "tenant-1",
		Key:      "nonexistent",
	})
	if err != ErrConfigNotFound {
		t.Fatalf("expected ErrConfigNotFound, got: %v", err)
	}
}

func TestSetConfigVersioning(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	_, err := svc.SetConfig(ctx, &configv1.SetConfigRequest{
		TenantId: "tenant-1",
		Key:      "app/db/host",
		Value:    "host1",
	})
	if err != nil {
		t.Fatalf("SetConfig failed: %v", err)
	}

	cfg, err := svc.SetConfig(ctx, &configv1.SetConfigRequest{
		TenantId: "tenant-1",
		Key:      "app/db/host",
		Value:    "host2",
	})
	if err != nil {
		t.Fatalf("SetConfig failed: %v", err)
	}

	if cfg.Version != 2 {
		t.Errorf("expected version 2, got %d", cfg.Version)
	}

	history, err := svc.GetConfigHistory(ctx, &configv1.GetConfigHistoryRequest{
		TenantId: "tenant-1",
		Key:      "app/db/host",
	})
	if err != nil {
		t.Fatalf("GetConfigHistory failed: %v", err)
	}

	if len(history.History) != 1 {
		t.Errorf("expected 1 history entry, got %d", len(history.History))
	}
}

func TestDeleteConfig(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	_, err := svc.SetConfig(ctx, &configv1.SetConfigRequest{
		TenantId: "tenant-1",
		Key:      "app/db/host",
		Value:    "localhost",
	})
	if err != nil {
		t.Fatalf("SetConfig failed: %v", err)
	}

	_, err = svc.DeleteConfig(ctx, &configv1.DeleteConfigRequest{
		TenantId: "tenant-1",
		Key:      "app/db/host",
	})
	if err != nil {
		t.Fatalf("DeleteConfig failed: %v", err)
	}

	_, err = svc.GetConfig(ctx, &configv1.GetConfigRequest{
		TenantId: "tenant-1",
		Key:      "app/db/host",
	})
	if err != ErrConfigNotFound {
		t.Fatalf("expected ErrConfigNotFound after delete, got: %v", err)
	}
}

func TestListConfigs(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	for i, key := range []string{"app/db/host", "app/db/port", "app/cache/ttl"} {
		_, err := svc.SetConfig(ctx, &configv1.SetConfigRequest{
			TenantId: "tenant-1",
			Key:      key,
			Value:    "value-" + string(rune('0'+i)),
		})
		if err != nil {
			t.Fatalf("SetConfig failed: %v", err)
		}
	}

	resp, err := svc.ListConfigs(ctx, &configv1.ListConfigsRequest{TenantId: "tenant-1"})
	if err != nil {
		t.Fatalf("ListConfigs failed: %v", err)
	}

	if len(resp.Configs) != 3 {
		t.Errorf("expected 3 configs, got %d", len(resp.Configs))
	}
}

func TestRollbackConfig(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	_, err := svc.SetConfig(ctx, &configv1.SetConfigRequest{
		TenantId: "tenant-1",
		Key:      "app/db/host",
		Value:    "host1",
	})
	if err != nil {
		t.Fatalf("SetConfig failed: %v", err)
	}

	_, err = svc.SetConfig(ctx, &configv1.SetConfigRequest{
		TenantId: "tenant-1",
		Key:      "app/db/host",
		Value:    "host2",
	})
	if err != nil {
		t.Fatalf("SetConfig failed: %v", err)
	}

	cfg, err := svc.RollbackConfig(ctx, &configv1.RollbackConfigRequest{
		TenantId: "tenant-1",
		Key:      "app/db/host",
		Version:  1,
	})
	if err != nil {
		t.Fatalf("RollbackConfig failed: %v", err)
	}

	if cfg.Value != "host1" {
		t.Errorf("expected value 'host1', got %s", cfg.Value)
	}
}

// ===================== Secret Tests =====================

func TestCreateSecret(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	meta, err := svc.CreateSecret(ctx, &configv1.CreateSecretRequest{
		TenantId: "tenant-1",
		Key:      "app/db/password",
		Value:    "super-secret",
		KeyType:  "passphrase",
	})
	if err != nil {
		t.Fatalf("CreateSecret failed: %v", err)
	}

	if meta.Id == "" {
		t.Error("expected secret ID to be set")
	}
	if meta.Version != 1 {
		t.Errorf("expected version 1, got %d", meta.Version)
	}
}

func TestGetSecret(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	_, err := svc.CreateSecret(ctx, &configv1.CreateSecretRequest{
		TenantId: "tenant-1",
		Key:      "app/db/password",
		Value:    "super-secret",
	})
	if err != nil {
		t.Fatalf("CreateSecret failed: %v", err)
	}

	item, err := svc.GetSecret(ctx, &configv1.GetSecretRequest{
		TenantId: "tenant-1",
		Key:      "app/db/password",
	})
	if err != nil {
		t.Fatalf("GetSecret failed: %v", err)
	}

	if item.Value != "super-secret" {
		t.Errorf("expected value 'super-secret', got %s", item.Value)
	}
}

func TestRotateSecret(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	_, err := svc.CreateSecret(ctx, &configv1.CreateSecretRequest{
		TenantId: "tenant-1",
		Key:      "app/db/password",
		Value:    "old-password",
	})
	if err != nil {
		t.Fatalf("CreateSecret failed: %v", err)
	}

	meta, err := svc.RotateSecret(ctx, &configv1.RotateSecretRequest{
		TenantId: "tenant-1",
		Key:      "app/db/password",
		NewValue: "new-password",
	})
	if err != nil {
		t.Fatalf("RotateSecret failed: %v", err)
	}

	if meta.Version != 2 {
		t.Errorf("expected version 2, got %d", meta.Version)
	}

	item, err := svc.GetSecret(ctx, &configv1.GetSecretRequest{
		TenantId: "tenant-1",
		Key:      "app/db/password",
	})
	if err != nil {
		t.Fatalf("GetSecret failed: %v", err)
	}

	if item.Value != "new-password" {
		t.Errorf("expected value 'new-password', got %s", item.Value)
	}
}

// ===================== Channel Tests =====================

func TestCreateAndGetChannel(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	ch, err := svc.CreateChannel(ctx, &configv1.CreateChannelRequest{
		TenantId: "tenant-1",
		Name:     "Ops Slack",
		Type:     "slack",
		Config:   `{"webhook_url":"https://hooks.slack.com/xxx"}`,
		Enabled:  true,
	})
	if err != nil {
		t.Fatalf("CreateChannel failed: %v", err)
	}

	if ch.Id == "" {
		t.Error("expected channel ID to be set")
	}

	got, err := svc.GetChannel(ctx, &configv1.GetChannelRequest{Id: ch.Id})
	if err != nil {
		t.Fatalf("GetChannel failed: %v", err)
	}

	if got.Name != "Ops Slack" {
		t.Errorf("expected name 'Ops Slack', got %s", got.Name)
	}
	if got.Type != "slack" {
		t.Errorf("expected type 'slack', got %s", got.Type)
	}
}

func TestTestChannel(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	ch, err := svc.CreateChannel(ctx, &configv1.CreateChannelRequest{
		TenantId: "tenant-1",
		Name:     "Ops Slack",
		Type:     "slack",
		Enabled:  true,
	})
	if err != nil {
		t.Fatalf("CreateChannel failed: %v", err)
	}

	resp, err := svc.TestChannel(ctx, &configv1.TestChannelRequest{Id: ch.Id})
	if err != nil {
		t.Fatalf("TestChannel failed: %v", err)
	}

	if !resp.Success {
		t.Error("expected test to succeed")
	}
}

func TestListChannels(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	for _, name := range []string{"Channel A", "Channel B"} {
		_, err := svc.CreateChannel(ctx, &configv1.CreateChannelRequest{
			TenantId: "tenant-1",
			Name:     name,
			Type:     "webhook",
			Enabled:  true,
		})
		if err != nil {
			t.Fatalf("CreateChannel failed: %v", err)
		}
	}

	resp, err := svc.ListChannels(ctx, &configv1.ListChannelsRequest{TenantId: "tenant-1"})
	if err != nil {
		t.Fatalf("ListChannels failed: %v", err)
	}

	if len(resp.Channels) != 2 {
		t.Errorf("expected 2 channels, got %d", len(resp.Channels))
	}
}

// ===================== Template Tests =====================

func TestCreateAndGetTemplate(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	tmpl, err := svc.CreateTemplate(ctx, &configv1.CreateTemplateRequest{
		TenantId:    "tenant-1",
		Name:        "DB Config",
		Description: "Database configuration template",
		Content:     "host={{.host}} port={{.port}}",
		Variables:   map[string]string{"host": "localhost", "port": "5432"},
	})
	if err != nil {
		t.Fatalf("CreateTemplate failed: %v", err)
	}

	if tmpl.Id == "" {
		t.Error("expected template ID to be set")
	}

	got, err := svc.GetTemplate(ctx, &configv1.GetTemplateRequest{Id: tmpl.Id})
	if err != nil {
		t.Fatalf("GetTemplate failed: %v", err)
	}

	if got.Name != "DB Config" {
		t.Errorf("expected name 'DB Config', got %s", got.Name)
	}
}

func TestApplyTemplate(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	tmpl, err := svc.CreateTemplate(ctx, &configv1.CreateTemplateRequest{
		TenantId:  "tenant-1",
		Name:      "DB Config",
		Content:   "host={{.host}} port={{.port}}",
		Variables: map[string]string{"host": "localhost", "port": "5432"},
	})
	if err != nil {
		t.Fatalf("CreateTemplate failed: %v", err)
	}

	resp, err := svc.ApplyTemplate(ctx, &configv1.ApplyTemplateRequest{
		Id:        tmpl.Id,
		Variables: map[string]string{"host": "db.example.com"},
	})
	if err != nil {
		t.Fatalf("ApplyTemplate failed: %v", err)
	}

	if resp.RenderedContent != "host=db.example.com port=5432" {
		t.Errorf("unexpected rendered content: %s", resp.RenderedContent)
	}
}
