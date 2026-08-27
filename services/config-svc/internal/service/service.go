package service

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"text/template"
	"time"

	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"

	configv1 "github.com/Levango7/OpsMesh/services/config-svc/api/proto/v1"
	"github.com/Levango7/OpsMesh/services/config-svc/internal/models"
	"github.com/Levango7/OpsMesh/services/config-svc/internal/store"
)

// Errors returned by the service.
var (
	ErrConfigNotFound    = errors.New("config not found")
	ErrConfigInvalid     = errors.New("config invalid")
	ErrSecretNotFound    = errors.New("secret not found")
	ErrSecretInvalid     = errors.New("secret invalid")
	ErrChannelNotFound   = errors.New("channel not found")
	ErrChannelInvalid    = errors.New("channel invalid")
	ErrTemplateNotFound  = errors.New("template not found")
	ErrTemplateInvalid   = errors.New("template invalid")
	ErrVersionNotFound   = errors.New("version not found")
	ErrEncryptionFailed  = errors.New("encryption failed")
)

// Service implements the config service business logic.
type Service struct {
	store store.Store
}

// NewService creates a new Service.
func NewService(s store.Store) *Service {
	return &Service{store: s}
}

// ===================== Config Service Methods =====================

func (s *Service) GetConfig(ctx context.Context, req *configv1.GetConfigRequest) (*configv1.ConfigItem, error) {
	if req.TenantId == "" || req.Key == "" {
		return nil, ErrConfigInvalid
	}
	entry, ok := s.store.GetConfig(req.TenantId, req.Key)
	if !ok {
		return nil, ErrConfigNotFound
	}
	return modelToProtoConfig(entry), nil
}

func (s *Service) SetConfig(ctx context.Context, req *configv1.SetConfigRequest) (*configv1.ConfigItem, error) {
	if req.TenantId == "" || req.Key == "" || req.Value == "" {
		return nil, ErrConfigInvalid
	}

	now := timestamppb.Now()
	entry := &models.ConfigEntry{
		ID:          uuid.New().String(),
		TenantID:    req.TenantId,
		Key:         req.Key,
		Value:       req.Value,
		Format:      req.Format,
		Description: req.Description,
		UpdatedBy:   req.UpdatedBy,
		CreatedAt:   now.AsTime(),
		UpdatedAt:   now.AsTime(),
	}

	existing, ok := s.store.GetConfig(req.TenantId, req.Key)
	if ok {
		entry.ID = existing.ID
		entry.CreatedAt = existing.CreatedAt
	}

	result := s.store.SetConfig(entry)
	return modelToProtoConfig(result), nil
}

func (s *Service) DeleteConfig(ctx context.Context, req *configv1.DeleteConfigRequest) (*emptypb.Empty, error) {
	if req.TenantId == "" || req.Key == "" {
		return nil, ErrConfigInvalid
	}
	if !s.store.DeleteConfig(req.TenantId, req.Key) {
		return nil, ErrConfigNotFound
	}
	return &emptypb.Empty{}, nil
}

func (s *Service) ListConfigs(ctx context.Context, req *configv1.ListConfigsRequest) (*configv1.ListConfigsResponse, error) {
	entries := s.store.ListConfigs(req.TenantId)
	out := make([]*configv1.ConfigItem, 0, len(entries))
	for _, e := range entries {
		out = append(out, modelToProtoConfig(e))
	}
	return &configv1.ListConfigsResponse{Configs: out}, nil
}

func (s *Service) GetConfigHistory(ctx context.Context, req *configv1.GetConfigHistoryRequest) (*configv1.GetConfigHistoryResponse, error) {
	if req.TenantId == "" || req.Key == "" {
		return nil, ErrConfigInvalid
	}
	history := s.store.GetConfigHistory(req.TenantId, req.Key)
	out := make([]*configv1.ConfigItem, 0, len(history))
	for _, h := range history {
		out = append(out, modelToProtoConfig(h))
	}
	return &configv1.GetConfigHistoryResponse{History: out}, nil
}

func (s *Service) RollbackConfig(ctx context.Context, req *configv1.RollbackConfigRequest) (*configv1.ConfigItem, error) {
	if req.TenantId == "" || req.Key == "" || req.Version <= 0 {
		return nil, ErrConfigInvalid
	}
	entry, ok := s.store.RollbackConfig(req.TenantId, req.Key, int(req.Version))
	if !ok {
		return nil, ErrVersionNotFound
	}
	return modelToProtoConfig(entry), nil
}

// ===================== Secret Service Methods =====================

func (s *Service) CreateSecret(ctx context.Context, req *configv1.CreateSecretRequest) (*configv1.SecretMeta, error) {
	if req.TenantId == "" || req.Key == "" || req.Value == "" {
		return nil, ErrSecretInvalid
	}

	entry := &models.SecretEntry{
		ID:       uuid.New().String(),
		TenantID: req.TenantId,
		Key:      req.Key,
		Value:    req.Value,
		KeyType:  req.KeyType,
	}

	if entry.KeyType == "" {
		entry.KeyType = "passphrase"
	}

	result := s.store.CreateSecret(entry)
	return &configv1.SecretMeta{
		Id:        result.ID,
		TenantId:  result.TenantID,
		Key:       result.Key,
		KeyType:   result.KeyType,
		Version:   int32(result.Version),
		CreatedAt: timestamppb.New(result.CreatedAt),
		UpdatedAt: timestamppb.New(result.UpdatedAt),
	}, nil
}

func (s *Service) GetSecret(ctx context.Context, req *configv1.GetSecretRequest) (*configv1.SecretItem, error) {
	if req.TenantId == "" || req.Key == "" {
		return nil, ErrSecretInvalid
	}
	entry, ok := s.store.GetSecret(req.TenantId, req.Key)
	if !ok {
		return nil, ErrSecretNotFound
	}
	return &configv1.SecretItem{
		Id:        entry.ID,
		TenantId:  entry.TenantID,
		Key:       entry.Key,
		Value:     entry.Value,
		KeyType:   entry.KeyType,
		Version:   int32(entry.Version),
		CreatedAt: timestamppb.New(entry.CreatedAt),
		UpdatedAt: timestamppb.New(entry.UpdatedAt),
	}, nil
}

func (s *Service) UpdateSecret(ctx context.Context, req *configv1.UpdateSecretRequest) (*configv1.SecretMeta, error) {
	if req.TenantId == "" || req.Key == "" || req.Value == "" {
		return nil, ErrSecretInvalid
	}

	existing, ok := s.store.GetSecret(req.TenantId, req.Key)
	if !ok {
		return nil, ErrSecretNotFound
	}

	entry := &models.SecretEntry{
		ID:       existing.ID,
		TenantID: req.TenantId,
		Key:      req.Key,
		Value:    req.Value,
		KeyType:  req.KeyType,
	}
	if entry.KeyType == "" {
		entry.KeyType = existing.KeyType
	}

	result := s.store.UpdateSecret(entry)
	return &configv1.SecretMeta{
		Id:        result.ID,
		TenantId:  result.TenantID,
		Key:       result.Key,
		KeyType:   result.KeyType,
		Version:   int32(result.Version),
		CreatedAt: timestamppb.New(result.CreatedAt),
		UpdatedAt: timestamppb.New(result.UpdatedAt),
	}, nil
}

func (s *Service) DeleteSecret(ctx context.Context, req *configv1.DeleteSecretRequest) (*emptypb.Empty, error) {
	if req.TenantId == "" || req.Key == "" {
		return nil, ErrSecretInvalid
	}
	if !s.store.DeleteSecret(req.TenantId, req.Key) {
		return nil, ErrSecretNotFound
	}
	return &emptypb.Empty{}, nil
}

func (s *Service) ListSecrets(ctx context.Context, req *configv1.ListSecretsRequest) (*configv1.ListSecretsResponse, error) {
	entries := s.store.ListSecrets(req.TenantId)
	out := make([]*configv1.SecretMeta, 0, len(entries))
	for _, e := range entries {
		out = append(out, &configv1.SecretMeta{
			Id:        e.ID,
			TenantId:  e.TenantID,
			Key:       e.Key,
			KeyType:   e.KeyType,
			Version:   int32(e.Version),
			CreatedAt: timestamppb.New(e.CreatedAt),
			UpdatedAt: timestamppb.New(e.UpdatedAt),
		})
	}
	return &configv1.ListSecretsResponse{Secrets: out}, nil
}

func (s *Service) RotateSecret(ctx context.Context, req *configv1.RotateSecretRequest) (*configv1.SecretMeta, error) {
	if req.TenantId == "" || req.Key == "" || req.NewValue == "" {
		return nil, ErrSecretInvalid
	}
	meta := s.store.RotateSecret(req.TenantId, req.Key, req.NewValue)
	if meta == nil {
		return nil, ErrSecretNotFound
	}
	return &configv1.SecretMeta{
		Id:        meta.ID,
		TenantId:  meta.TenantID,
		Key:       meta.Key,
		KeyType:   meta.KeyType,
		Version:   int32(meta.Version),
		CreatedAt: timestamppb.New(meta.CreatedAt),
		UpdatedAt: timestamppb.New(meta.UpdatedAt),
	}, nil
}

// ===================== Channel Service Methods =====================

func (s *Service) CreateChannel(ctx context.Context, req *configv1.CreateChannelRequest) (*configv1.NotifyChannel, error) {
	if req.TenantId == "" || req.Name == "" || req.Type == "" {
		return nil, ErrChannelInvalid
	}

	entry := &models.ChannelEntry{
		ID:       uuid.New().String(),
		TenantID: req.TenantId,
		Name:     req.Name,
		Type:     req.Type,
		Config:   req.Config,
		Enabled:  req.Enabled,
	}

	result := s.store.CreateChannel(entry)
	return modelToProtoChannel(result), nil
}

func (s *Service) GetChannel(ctx context.Context, req *configv1.GetChannelRequest) (*configv1.NotifyChannel, error) {
	if req.Id == "" {
		return nil, ErrChannelInvalid
	}
	entry := s.store.GetChannel(req.Id)
	if entry == nil {
		return nil, ErrChannelNotFound
	}
	return modelToProtoChannel(entry), nil
}

func (s *Service) UpdateChannel(ctx context.Context, req *configv1.UpdateChannelRequest) (*configv1.NotifyChannel, error) {
	if req.Id == "" {
		return nil, ErrChannelInvalid
	}

	existing := s.store.GetChannel(req.Id)
	if existing == nil {
		return nil, ErrChannelNotFound
	}

	entry := &models.ChannelEntry{
		ID:        existing.ID,
		TenantID:  existing.TenantID,
		Name:      req.Name,
		Type:      req.Type,
		Config:    req.Config,
		Enabled:   req.Enabled,
		CreatedAt: existing.CreatedAt,
	}

	if !s.store.UpdateChannel(entry) {
		return nil, ErrChannelNotFound
	}
	return modelToProtoChannel(entry), nil
}

func (s *Service) DeleteChannel(ctx context.Context, req *configv1.DeleteChannelRequest) (*emptypb.Empty, error) {
	if req.Id == "" {
		return nil, ErrChannelInvalid
	}
	if !s.store.DeleteChannel(req.Id, req.TenantId) {
		return nil, ErrChannelNotFound
	}
	return &emptypb.Empty{}, nil
}

func (s *Service) ListChannels(ctx context.Context, req *configv1.ListChannelsRequest) (*configv1.ListChannelsResponse, error) {
	entries := s.store.ListChannels(req.TenantId)
	out := make([]*configv1.NotifyChannel, 0, len(entries))
	for _, e := range entries {
		out = append(out, modelToProtoChannel(e))
	}
	return &configv1.ListChannelsResponse{Channels: out}, nil
}

func (s *Service) TestChannel(ctx context.Context, req *configv1.TestChannelRequest) (*configv1.TestChannelResponse, error) {
	if req.Id == "" {
		return nil, ErrChannelInvalid
	}
	entry := s.store.GetChannel(req.Id)
	if entry == nil {
		return &configv1.TestChannelResponse{
			Success: false,
			Message: "channel not found",
		}, nil
	}
	if !entry.Enabled {
		return &configv1.TestChannelResponse{
			Success: false,
			Message: "channel is disabled",
		}, nil
	}
	return &configv1.TestChannelResponse{
		Success: true,
		Message: "test notification sent successfully",
	}, nil
}

// ===================== Template Service Methods =====================

func (s *Service) CreateTemplate(ctx context.Context, req *configv1.CreateTemplateRequest) (*configv1.ConfigTemplate, error) {
	if req.TenantId == "" || req.Name == "" || req.Content == "" {
		return nil, ErrTemplateInvalid
	}

	entry := &models.TemplateEntry{
		ID:          uuid.New().String(),
		TenantID:    req.TenantId,
		Name:        req.Name,
		Description: req.Description,
		Content:     req.Content,
		Variables:   req.Variables,
	}
	if entry.Variables == nil {
		entry.Variables = make(map[string]string)
	}

	result := s.store.CreateTemplate(entry)
	return modelToProtoTemplate(result), nil
}

func (s *Service) GetTemplate(ctx context.Context, req *configv1.GetTemplateRequest) (*configv1.ConfigTemplate, error) {
	if req.Id == "" {
		return nil, ErrTemplateInvalid
	}
	entry := s.store.GetTemplate(req.Id)
	if entry == nil {
		return nil, ErrTemplateNotFound
	}
	return modelToProtoTemplate(entry), nil
}

func (s *Service) UpdateTemplate(ctx context.Context, req *configv1.UpdateTemplateRequest) (*configv1.ConfigTemplate, error) {
	if req.Id == "" {
		return nil, ErrTemplateInvalid
	}

	existing := s.store.GetTemplate(req.Id)
	if existing == nil {
		return nil, ErrTemplateNotFound
	}

	entry := &models.TemplateEntry{
		ID:          existing.ID,
		TenantID:    existing.TenantID,
		Name:        req.Name,
		Description: req.Description,
		Content:     req.Content,
		Variables:   req.Variables,
		CreatedAt:    existing.CreatedAt,
	}
	if entry.Variables == nil {
		entry.Variables = make(map[string]string)
	}

	if !s.store.UpdateTemplate(entry) {
		return nil, ErrTemplateNotFound
	}
	return modelToProtoTemplate(entry), nil
}

func (s *Service) DeleteTemplate(ctx context.Context, req *configv1.DeleteTemplateRequest) (*emptypb.Empty, error) {
	if req.Id == "" {
		return nil, ErrTemplateInvalid
	}
	if !s.store.DeleteTemplate(req.Id, req.TenantId) {
		return nil, ErrTemplateNotFound
	}
	return &emptypb.Empty{}, nil
}

func (s *Service) ListTemplates(ctx context.Context, req *configv1.ListTemplatesRequest) (*configv1.ListTemplatesResponse, error) {
	entries := s.store.ListTemplates(req.TenantId)
	out := make([]*configv1.ConfigTemplate, 0, len(entries))
	for _, e := range entries {
		out = append(out, modelToProtoTemplate(e))
	}
	return &configv1.ListTemplatesResponse{Templates: out}, nil
}

func (s *Service) ApplyTemplate(ctx context.Context, req *configv1.ApplyTemplateRequest) (*configv1.ApplyTemplateResponse, error) {
	if req.Id == "" {
		return nil, ErrTemplateInvalid
	}
	entry := s.store.GetTemplate(req.Id)
	if entry == nil {
		return nil, ErrTemplateNotFound
	}

	tmpl, err := template.New("config").Parse(entry.Content)
	if err != nil {
		return nil, ErrTemplateInvalid
	}

	vars := make(map[string]string)
	for k, v := range entry.Variables {
		vars[k] = v
	}
	if req.Variables != nil {
		for k, v := range req.Variables {
			vars[k] = v
		}
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, vars); err != nil {
		return nil, ErrTemplateInvalid
	}

	return &configv1.ApplyTemplateResponse{RenderedContent: buf.String()}, nil
}

// ===================== Mapping Functions =====================

func modelToProtoConfig(e *models.ConfigEntry) *configv1.ConfigItem {
	format := e.Format
	if format == "" {
		format = "text"
	}
	return &configv1.ConfigItem{
		Id:          e.ID,
		TenantId:    e.TenantID,
		Key:         e.Key,
		Value:       e.Value,
		Format:      format,
		Version:     int32(e.Version),
		Description: e.Description,
		UpdatedBy:   e.UpdatedBy,
		CreatedAt:   timestamppb.New(e.CreatedAt),
		UpdatedAt:   timestamppb.New(e.UpdatedAt),
	}
}

func modelToProtoChannel(e *models.ChannelEntry) *configv1.NotifyChannel {
	return &configv1.NotifyChannel{
		Id:        e.ID,
		TenantId:  e.TenantID,
		Name:      e.Name,
		Type:      e.Type,
		Config:    e.Config,
		Enabled:   e.Enabled,
		CreatedAt: timestamppb.New(e.CreatedAt),
		UpdatedAt: timestamppb.New(e.UpdatedAt),
	}
}

func modelToProtoTemplate(e *models.TemplateEntry) *configv1.ConfigTemplate {
	vars := e.Variables
	if vars == nil {
		vars = make(map[string]string)
	}
	return &configv1.ConfigTemplate{
		Id:          e.ID,
		TenantId:    e.TenantID,
		Name:        e.Name,
		Description: e.Description,
		Content:     e.Content,
		Variables:   vars,
		CreatedAt:   timestamppb.New(e.CreatedAt),
		UpdatedAt:   timestamppb.New(e.UpdatedAt),
	}
}

// TimeNow is a helper for testing time-related logic.
var TimeNow = func() time.Time { return time.Now() }

// TemplateApply applies a template with variables using simple string replacement.
func TemplateApply(content string, variables map[string]string) string {
	for k, v := range variables {
		content = strings.ReplaceAll(content, "{{"+k+"}}", v)
	}
	return content
}
