package aigateway

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type recordingEmbedProvider struct {
	name       string
	embedCalls int
}

func (p *recordingEmbedProvider) Name() string { return p.name }
func (p *recordingEmbedProvider) Complete(context.Context, CompletionRequest) (*CompletionResponse, error) {
	return nil, nil
}
func (p *recordingEmbedProvider) Embed(_ context.Context, text string) ([]float64, error) {
	p.embedCalls++
	return []float64{float64(len(text)), 1}, nil
}
func (p *recordingEmbedProvider) ListModels() []string              { return []string{"m"} }
func (p *recordingEmbedProvider) HealthCheck(context.Context) error { return nil }

func newEmbedProviderTestHandler() (*AIGatewayHandler, *recordingEmbedProvider, *recordingEmbedProvider) {
	g := NewGateway()
	openai := &recordingEmbedProvider{name: "openai"}
	local := &recordingEmbedProvider{name: "local"}
	g.RegisterProvider(ProviderConfig{Name: "openai", Models: []string{"m"}}, openai)
	g.RegisterProvider(ProviderConfig{Name: "local", Models: []string{"m"}}, local)
	return NewAIGatewayHandler(g, NewInMemoryVectorStore(), 5), openai, local
}

func TestHandleEmbedDefaultProviderCanBeConfigured(t *testing.T) {
	h, openai, local := newEmbedProviderTestHandler()
	h.SetEmbedProvider("local")
	h.SetEmbedProvider("  ") // 空值不得把已配置的 Provider 清掉。

	req := httptest.NewRequest(http.MethodPost, "/api/v1/ai/embed", strings.NewReader(`{"text":"abc"}`))
	rec := httptest.NewRecorder()
	h.HandleEmbed(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("Embed status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if local.embedCalls != 1 || openai.embedCalls != 0 {
		t.Fatalf("默认 Embed 应走 local: local=%d openai=%d", local.embedCalls, openai.embedCalls)
	}
}

func TestHandleRAGIngestUsesConfiguredEmbedProvider(t *testing.T) {
	h, openai, local := newEmbedProviderTestHandler()
	h.SetEmbedProvider("local")
	body := `{"documents":[{"id":"doc-1","content":"four"}]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/ai/rag/ingest", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.HandleRAGIngest(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("RAG ingest status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if local.embedCalls != 1 || openai.embedCalls != 0 {
		t.Fatalf("RAG 自动向量应走 local: local=%d openai=%d", local.embedCalls, openai.embedCalls)
	}
	count, err := h.retriever.vectorStore.Count(context.Background())
	if err != nil || count != 1 {
		t.Fatalf("RAG 文档应写入当前租户: count=%d err=%v", count, err)
	}
}

func TestHandleEmbedKeepsOpenAIDefaultWithoutConfiguration(t *testing.T) {
	h, openai, local := newEmbedProviderTestHandler()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/ai/embed", strings.NewReader(`{"text":"abc"}`))
	rec := httptest.NewRecorder()
	h.HandleEmbed(rec, req)
	if rec.Code != http.StatusOK || openai.embedCalls != 1 || local.embedCalls != 0 {
		t.Fatalf("未配置时必须保持 openai 旧默认: status=%d openai=%d local=%d", rec.Code, openai.embedCalls, local.embedCalls)
	}
}
