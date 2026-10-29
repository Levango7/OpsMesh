package server

import (
	"context"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	configv1 "github.com/Levango7/OpsMesh/services/config-svc/api/proto/v1"
	"github.com/Levango7/OpsMesh/services/config-svc/internal/service"
	"github.com/Levango7/OpsMesh/services/config-svc/internal/store"
)

func newTestServer(t *testing.T) *Server {
	t.Helper()
	return NewServer(service.NewService(store.NewMemoryStore("test-key", 10)))
}

func requireCode(t *testing.T, err error, want codes.Code, what string) {
	t.Helper()
	if got := status.Code(err); got != want {
		t.Fatalf("%s: code = %v, want %v (err=%v)", what, got, want, err)
	}
}

func TestConfigHandlersMapping(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()

	// 参数不全 ⇒ InvalidArgument（而非 NotFound）。
	_, err := srv.GetConfig(ctx, &configv1.GetConfigRequest{TenantId: "t1"})
	requireCode(t, err, codes.InvalidArgument, "GetConfig(空 key)")
	_, err = srv.SetConfig(ctx, &configv1.SetConfigRequest{TenantId: "t1", Key: "k"})
	requireCode(t, err, codes.InvalidArgument, "SetConfig(空 value)")
	_, err = srv.DeleteConfig(ctx, &configv1.DeleteConfigRequest{TenantId: "t1"})
	requireCode(t, err, codes.InvalidArgument, "DeleteConfig(空 key)")
	_, err = srv.GetConfigHistory(ctx, &configv1.GetConfigHistoryRequest{TenantId: "t1"})
	requireCode(t, err, codes.InvalidArgument, "GetConfigHistory(空 key)")

	// 不存在 ⇒ NotFound。
	_, err = srv.GetConfig(ctx, &configv1.GetConfigRequest{TenantId: "t1", Key: "missing"})
	requireCode(t, err, codes.NotFound, "GetConfig(missing)")
	_, err = srv.DeleteConfig(ctx, &configv1.DeleteConfigRequest{TenantId: "t1", Key: "missing"})
	requireCode(t, err, codes.NotFound, "DeleteConfig(missing)")

	first, err := srv.SetConfig(ctx, &configv1.SetConfigRequest{
		TenantId: "t1", Key: "app.log_level", Value: "info",
	})
	if err != nil {
		t.Fatalf("SetConfig: %v", err)
	}
	if first.GetVersion() != 1 || first.GetFormat() != "text" {
		t.Fatalf("SetConfig 首次 = %+v, want version=1 format=text", first)
	}

	second, err := srv.SetConfig(ctx, &configv1.SetConfigRequest{
		TenantId: "t1", Key: "app.log_level", Value: "debug", Format: "text",
	})
	if err != nil {
		t.Fatalf("SetConfig(更新): %v", err)
	}
	if second.GetVersion() != 2 || second.GetId() != first.GetId() {
		t.Fatalf("SetConfig 更新 = %+v, want version=2 且 ID 保持不变(%s)", second, first.GetId())
	}

	got, err := srv.GetConfig(ctx, &configv1.GetConfigRequest{TenantId: "t1", Key: "app.log_level"})
	if err != nil {
		t.Fatalf("GetConfig: %v", err)
	}
	if got.GetValue() != "debug" {
		t.Fatalf("GetConfig value = %q, want debug", got.GetValue())
	}

	hist, err := srv.GetConfigHistory(ctx, &configv1.GetConfigHistoryRequest{TenantId: "t1", Key: "app.log_level"})
	if err != nil {
		t.Fatalf("GetConfigHistory: %v", err)
	}
	if len(hist.GetHistory()) != 1 || hist.GetHistory()[0].GetValue() != "info" {
		t.Fatalf("GetConfigHistory = %+v, want 单条 value=info", hist.GetHistory())
	}

	// 版本号非法 ⇒ InvalidArgument；版本不存在 ⇒ NotFound。
	_, err = srv.RollbackConfig(ctx, &configv1.RollbackConfigRequest{TenantId: "t1", Key: "app.log_level"})
	requireCode(t, err, codes.InvalidArgument, "RollbackConfig(version=0)")
	_, err = srv.RollbackConfig(ctx, &configv1.RollbackConfigRequest{TenantId: "t1", Key: "app.log_level", Version: 99})
	requireCode(t, err, codes.NotFound, "RollbackConfig(version=99)")

	rolled, err := srv.RollbackConfig(ctx, &configv1.RollbackConfigRequest{TenantId: "t1", Key: "app.log_level", Version: 1})
	if err != nil {
		t.Fatalf("RollbackConfig: %v", err)
	}
	if rolled.GetValue() != "info" || rolled.GetVersion() != 3 {
		t.Fatalf("RollbackConfig = value=%q version=%d, want info/3", rolled.GetValue(), rolled.GetVersion())
	}

	if _, err := srv.DeleteConfig(ctx, &configv1.DeleteConfigRequest{TenantId: "t1", Key: "app.log_level"}); err != nil {
		t.Fatalf("DeleteConfig: %v", err)
	}
}

func TestListConfigsPassthrough(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()

	resp, err := srv.ListConfigs(ctx, &configv1.ListConfigsRequest{TenantId: "t1"})
	if err != nil {
		t.Fatalf("ListConfigs: %v", err)
	}
	if resp.GetConfigs() == nil {
		t.Fatal("ListConfigs 空结果应是空切片而非 nil（JSON 需序列化为 []）")
	}

	for _, kv := range [][2]string{{"t1", "b"}, {"t1", "a"}, {"t2", "c"}} {
		if _, err := srv.SetConfig(ctx, &configv1.SetConfigRequest{TenantId: kv[0], Key: kv[1], Value: "v"}); err != nil {
			t.Fatalf("SetConfig(%v): %v", kv, err)
		}
	}
	resp, err = srv.ListConfigs(ctx, &configv1.ListConfigsRequest{TenantId: "t1"})
	if err != nil {
		t.Fatalf("ListConfigs: %v", err)
	}
	if len(resp.GetConfigs()) != 2 {
		t.Fatalf("ListConfigs(t1) = %d 条, want 2", len(resp.GetConfigs()))
	}
	if resp.GetConfigs()[0].GetKey() != "a" || resp.GetConfigs()[1].GetKey() != "b" {
		t.Fatalf("ListConfigs 未按 key 排序: %q,%q", resp.GetConfigs()[0].GetKey(), resp.GetConfigs()[1].GetKey())
	}
}

func TestSecretHandlersMapping(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()

	_, err := srv.CreateSecret(ctx, &configv1.CreateSecretRequest{TenantId: "t1", Key: "db"})
	requireCode(t, err, codes.InvalidArgument, "CreateSecret(空 value)")
	_, err = srv.GetSecret(ctx, &configv1.GetSecretRequest{TenantId: "t1"})
	requireCode(t, err, codes.InvalidArgument, "GetSecret(空 key)")
	_, err = srv.UpdateSecret(ctx, &configv1.UpdateSecretRequest{TenantId: "t1", Key: "db"})
	requireCode(t, err, codes.InvalidArgument, "UpdateSecret(空 value)")
	_, err = srv.DeleteSecret(ctx, &configv1.DeleteSecretRequest{Key: "db"})
	requireCode(t, err, codes.InvalidArgument, "DeleteSecret(空 tenant)")
	_, err = srv.RotateSecret(ctx, &configv1.RotateSecretRequest{TenantId: "t1", Key: "db"})
	requireCode(t, err, codes.InvalidArgument, "RotateSecret(空 new_value)")

	_, err = srv.GetSecret(ctx, &configv1.GetSecretRequest{TenantId: "t1", Key: "missing"})
	requireCode(t, err, codes.NotFound, "GetSecret(missing)")
	_, err = srv.UpdateSecret(ctx, &configv1.UpdateSecretRequest{TenantId: "t1", Key: "missing", Value: "x"})
	requireCode(t, err, codes.NotFound, "UpdateSecret(missing)")
	_, err = srv.DeleteSecret(ctx, &configv1.DeleteSecretRequest{TenantId: "t1", Key: "missing"})
	requireCode(t, err, codes.NotFound, "DeleteSecret(missing)")
	_, err = srv.RotateSecret(ctx, &configv1.RotateSecretRequest{TenantId: "t1", Key: "missing", NewValue: "x"})
	requireCode(t, err, codes.NotFound, "RotateSecret(missing)")

	meta, err := srv.CreateSecret(ctx, &configv1.CreateSecretRequest{TenantId: "t1", Key: "db", Value: "s3cret"})
	if err != nil {
		t.Fatalf("CreateSecret: %v", err)
	}
	if meta.GetKeyType() != "passphrase" || meta.GetVersion() != 1 {
		t.Fatalf("CreateSecret = key_type=%q version=%d, want passphrase/1", meta.GetKeyType(), meta.GetVersion())
	}

	// 关键：适配层必须返回解密后的明文，而不是密文。
	item, err := srv.GetSecret(ctx, &configv1.GetSecretRequest{TenantId: "t1", Key: "db"})
	if err != nil {
		t.Fatalf("GetSecret: %v", err)
	}
	if item.GetValue() != "s3cret" {
		t.Fatalf("GetSecret value = %q, want 明文 s3cret", item.GetValue())
	}

	updated, err := srv.UpdateSecret(ctx, &configv1.UpdateSecretRequest{TenantId: "t1", Key: "db", Value: "s3cret-2"})
	if err != nil {
		t.Fatalf("UpdateSecret: %v", err)
	}
	if updated.GetVersion() != 2 || updated.GetKeyType() != "passphrase" {
		t.Fatalf("UpdateSecret = version=%d key_type=%q, want 2/passphrase(沿用)", updated.GetVersion(), updated.GetKeyType())
	}

	rotated, err := srv.RotateSecret(ctx, &configv1.RotateSecretRequest{TenantId: "t1", Key: "db", NewValue: "s3cret-3"})
	if err != nil {
		t.Fatalf("RotateSecret: %v", err)
	}
	if rotated.GetVersion() != 3 {
		t.Fatalf("RotateSecret version = %d, want 3", rotated.GetVersion())
	}
	item, err = srv.GetSecret(ctx, &configv1.GetSecretRequest{TenantId: "t1", Key: "db"})
	if err != nil {
		t.Fatalf("GetSecret(轮换后): %v", err)
	}
	if item.GetValue() != "s3cret-3" {
		t.Fatalf("轮换后 value = %q, want s3cret-3", item.GetValue())
	}

	list, err := srv.ListSecrets(ctx, &configv1.ListSecretsRequest{TenantId: "t1"})
	if err != nil {
		t.Fatalf("ListSecrets: %v", err)
	}
	if len(list.GetSecrets()) != 1 || list.GetSecrets()[0].GetKey() != "db" {
		t.Fatalf("ListSecrets = %+v, want 单条 db", list.GetSecrets())
	}

	if _, err := srv.DeleteSecret(ctx, &configv1.DeleteSecretRequest{TenantId: "t1", Key: "db"}); err != nil {
		t.Fatalf("DeleteSecret: %v", err)
	}
}

func TestChannelHandlersMapping(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()

	_, err := srv.CreateChannel(ctx, &configv1.CreateChannelRequest{TenantId: "t1", Name: "ops"})
	requireCode(t, err, codes.InvalidArgument, "CreateChannel(空 type)")
	_, err = srv.GetChannel(ctx, &configv1.GetChannelRequest{})
	requireCode(t, err, codes.InvalidArgument, "GetChannel(空 id)")
	_, err = srv.UpdateChannel(ctx, &configv1.UpdateChannelRequest{})
	requireCode(t, err, codes.InvalidArgument, "UpdateChannel(空 id)")
	_, err = srv.DeleteChannel(ctx, &configv1.DeleteChannelRequest{})
	requireCode(t, err, codes.InvalidArgument, "DeleteChannel(空 id)")
	_, err = srv.TestChannel(ctx, &configv1.TestChannelRequest{})
	requireCode(t, err, codes.InvalidArgument, "TestChannel(空 id)")

	_, err = srv.GetChannel(ctx, &configv1.GetChannelRequest{Id: "missing"})
	requireCode(t, err, codes.NotFound, "GetChannel(missing)")
	_, err = srv.UpdateChannel(ctx, &configv1.UpdateChannelRequest{Id: "missing"})
	requireCode(t, err, codes.NotFound, "UpdateChannel(missing)")
	_, err = srv.DeleteChannel(ctx, &configv1.DeleteChannelRequest{Id: "missing"})
	requireCode(t, err, codes.NotFound, "DeleteChannel(missing)")

	ch, err := srv.CreateChannel(ctx, &configv1.CreateChannelRequest{
		TenantId: "t1", Name: "ops", Type: "webhook", Config: `{"url":"http://x"}`, Enabled: true,
	})
	if err != nil {
		t.Fatalf("CreateChannel: %v", err)
	}

	// 渠道不存在时 TestChannel 走业务返回（Success=false）而不是 gRPC 错误。
	testResp, err := srv.TestChannel(ctx, &configv1.TestChannelRequest{Id: "missing"})
	if err != nil {
		t.Fatalf("TestChannel(missing) 不应返回 gRPC 错误: %v", err)
	}
	if testResp.GetSuccess() || testResp.GetMessage() != "channel not found" {
		t.Fatalf("TestChannel(missing) = %+v", testResp)
	}

	testResp, err = srv.TestChannel(ctx, &configv1.TestChannelRequest{Id: ch.GetId()})
	if err != nil {
		t.Fatalf("TestChannel: %v", err)
	}
	if !testResp.GetSuccess() {
		t.Fatalf("TestChannel(enabled) = %+v, want success", testResp)
	}

	updated, err := srv.UpdateChannel(ctx, &configv1.UpdateChannelRequest{
		Id: ch.GetId(), Name: "ops-2", Type: "webhook", Enabled: false,
	})
	if err != nil {
		t.Fatalf("UpdateChannel: %v", err)
	}
	if updated.GetName() != "ops-2" || updated.GetTenantId() != "t1" {
		t.Fatalf("UpdateChannel = %+v, want name=ops-2 且沿用 tenant", updated)
	}

	testResp, err = srv.TestChannel(ctx, &configv1.TestChannelRequest{Id: ch.GetId()})
	if err != nil {
		t.Fatalf("TestChannel(disabled): %v", err)
	}
	if testResp.GetSuccess() || testResp.GetMessage() != "channel is disabled" {
		t.Fatalf("TestChannel(disabled) = %+v", testResp)
	}

	list, err := srv.ListChannels(ctx, &configv1.ListChannelsRequest{TenantId: "t2"})
	if err != nil {
		t.Fatalf("ListChannels: %v", err)
	}
	if len(list.GetChannels()) != 0 {
		t.Fatalf("ListChannels(t2) = %d 条, want 0（租户隔离）", len(list.GetChannels()))
	}

	if _, err := srv.DeleteChannel(ctx, &configv1.DeleteChannelRequest{Id: ch.GetId(), TenantId: "t1"}); err != nil {
		t.Fatalf("DeleteChannel: %v", err)
	}
}

func TestTemplateHandlersMapping(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()

	_, err := srv.CreateTemplate(ctx, &configv1.CreateTemplateRequest{TenantId: "t1", Name: "nginx"})
	requireCode(t, err, codes.InvalidArgument, "CreateTemplate(空 content)")
	_, err = srv.GetTemplate(ctx, &configv1.GetTemplateRequest{})
	requireCode(t, err, codes.InvalidArgument, "GetTemplate(空 id)")
	_, err = srv.UpdateTemplate(ctx, &configv1.UpdateTemplateRequest{})
	requireCode(t, err, codes.InvalidArgument, "UpdateTemplate(空 id)")
	_, err = srv.DeleteTemplate(ctx, &configv1.DeleteTemplateRequest{})
	requireCode(t, err, codes.InvalidArgument, "DeleteTemplate(空 id)")
	_, err = srv.ApplyTemplate(ctx, &configv1.ApplyTemplateRequest{})
	requireCode(t, err, codes.InvalidArgument, "ApplyTemplate(空 id)")

	_, err = srv.GetTemplate(ctx, &configv1.GetTemplateRequest{Id: "missing"})
	requireCode(t, err, codes.NotFound, "GetTemplate(missing)")
	_, err = srv.UpdateTemplate(ctx, &configv1.UpdateTemplateRequest{Id: "missing"})
	requireCode(t, err, codes.NotFound, "UpdateTemplate(missing)")
	_, err = srv.DeleteTemplate(ctx, &configv1.DeleteTemplateRequest{Id: "missing"})
	requireCode(t, err, codes.NotFound, "DeleteTemplate(missing)")
	_, err = srv.ApplyTemplate(ctx, &configv1.ApplyTemplateRequest{Id: "missing"})
	requireCode(t, err, codes.NotFound, "ApplyTemplate(missing)")

	// Variables 为 nil 时服务层补空 map；适配层不应把 nil 透出。
	tmpl, err := srv.CreateTemplate(ctx, &configv1.CreateTemplateRequest{
		TenantId: "t1", Name: "nginx", Content: "listen {{.port}};",
	})
	if err != nil {
		t.Fatalf("CreateTemplate: %v", err)
	}
	if tmpl.GetVariables() == nil {
		t.Fatal("CreateTemplate 返回的 variables 不应为 nil")
	}

	applied, err := srv.ApplyTemplate(ctx, &configv1.ApplyTemplateRequest{
		Id: tmpl.GetId(), Variables: map[string]string{"port": "8080"},
	})
	if err != nil {
		t.Fatalf("ApplyTemplate: %v", err)
	}
	if applied.GetRenderedContent() != "listen 8080;" {
		t.Fatalf("ApplyTemplate = %q, want %q", applied.GetRenderedContent(), "listen 8080;")
	}

	// 模板语法错误 ⇒ InvalidArgument（不是 NotFound/Internal）。
	bad, err := srv.CreateTemplate(ctx, &configv1.CreateTemplateRequest{
		TenantId: "t1", Name: "bad", Content: "{{.port",
	})
	if err != nil {
		t.Fatalf("CreateTemplate(bad): %v", err)
	}
	_, err = srv.ApplyTemplate(ctx, &configv1.ApplyTemplateRequest{Id: bad.GetId()})
	requireCode(t, err, codes.InvalidArgument, "ApplyTemplate(模板语法错误)")

	updated, err := srv.UpdateTemplate(ctx, &configv1.UpdateTemplateRequest{
		Id: tmpl.GetId(), Name: "nginx-2", Content: "server {{.port}};",
	})
	if err != nil {
		t.Fatalf("UpdateTemplate: %v", err)
	}
	if updated.GetName() != "nginx-2" || updated.GetTenantId() != "t1" {
		t.Fatalf("UpdateTemplate = %+v, want name=nginx-2 且沿用 tenant", updated)
	}

	list, err := srv.ListTemplates(ctx, &configv1.ListTemplatesRequest{TenantId: "t1"})
	if err != nil {
		t.Fatalf("ListTemplates: %v", err)
	}
	if len(list.GetTemplates()) != 2 {
		t.Fatalf("ListTemplates = %d 条, want 2", len(list.GetTemplates()))
	}

	if _, err := srv.DeleteTemplate(ctx, &configv1.DeleteTemplateRequest{Id: tmpl.GetId(), TenantId: "t1"}); err != nil {
		t.Fatalf("DeleteTemplate: %v", err)
	}
}
