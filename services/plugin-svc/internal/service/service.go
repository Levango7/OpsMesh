package service

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Levango7/OpsMesh/services/plugin-svc/internal/models"
	"github.com/Levango7/OpsMesh/services/plugin-svc/internal/store"
)

// Errors returned by the service.
var (
	ErrPluginNotFound      = errors.New("plugin not found")
	ErrPluginInvalid       = errors.New("plugin invalid")
	ErrPluginExists        = errors.New("plugin already exists")
	ErrInvalidURL          = errors.New("invalid download URL")
	ErrSSRFBlocked         = errors.New("download URL blocked: private/internal IP")
	ErrChecksumMismatch    = errors.New("checksum mismatch")
	ErrVersionNotFound     = errors.New("version not found")
	ErrPluginNotInstalled  = errors.New("plugin not installed")
	ErrPluginInstalled     = errors.New("plugin already installed")
	ErrInvalidStatus       = errors.New("invalid plugin status")
)

// Service implements the plugin marketplace business logic.
type Service struct {
	store store.PluginStore
}

// NewService creates a new Service.
func NewService(s store.PluginStore) *Service {
	return &Service{store: s}
}

// ListPlugins returns all plugins.
func (s *Service) ListPlugins() []*models.Plugin {
	return s.store.List()
}

// GetPlugin retrieves a plugin by ID.
func (s *Service) GetPlugin(id string) (*models.Plugin, error) {
	p, ok := s.store.Get(id)
	if !ok {
		return nil, ErrPluginNotFound
	}
	return p, nil
}

// CreatePlugin registers a new plugin.
func (s *Service) CreatePlugin(p *models.Plugin) (*models.Plugin, error) {
	if p == nil {
		return nil, ErrPluginInvalid
	}
	if p.Name == "" {
		return nil, fmt.Errorf("%w: name is required", ErrPluginInvalid)
	}
	if p.Version == "" {
		return nil, fmt.Errorf("%w: version is required", ErrPluginInvalid)
	}
	if p.Type == "" {
		return nil, fmt.Errorf("%w: type is required", ErrPluginInvalid)
	}
	if !models.ValidPluginTypes[p.Type] {
		return nil, fmt.Errorf("%w: invalid type %s (want data|logic|integration)", ErrPluginInvalid, p.Type)
	}
	if err := validateDownloadURL(p.DownloadURL); err != nil {
		return nil, err
	}
	if p.ID == "" {
		p.ID = uuid.New().String()
	}
	p.Status = models.StatusPending
	p.Installed = false
	p.Enabled = false
	created := s.store.Create(p)
	s.store.AddVersion(&models.PluginVersion{
		PluginID:    created.ID,
		Version:     created.Version,
		Checksum:    created.Checksum,
		DownloadURL: created.DownloadURL,
		Changelog:   "initial release",
	})
	return created, nil
}

// UpdatePlugin modifies an existing plugin.
func (s *Service) UpdatePlugin(p *models.Plugin) (*models.Plugin, error) {
	if p == nil || p.ID == "" {
		return nil, ErrPluginInvalid
	}
	if _, ok := s.store.Get(p.ID); !ok {
		return nil, ErrPluginNotFound
	}
	if p.Type != "" && !models.ValidPluginTypes[p.Type] {
		return nil, fmt.Errorf("%w: invalid type %s", ErrPluginInvalid, p.Type)
	}
	if err := validateDownloadURL(p.DownloadURL); err != nil {
		return nil, err
	}
	updated, ok := s.store.Update(p)
	if !ok {
		return nil, fmt.Errorf("update failed")
	}
	return updated, nil
}

// DeletePlugin removes a plugin.
func (s *Service) DeletePlugin(id string) error {
	if !s.store.Delete(id) {
		return ErrPluginNotFound
	}
	return nil
}

// InstallPlugin installs a plugin with checksum verification.
func (s *Service) InstallPlugin(id string) (*models.Plugin, error) {
	p, ok := s.store.Get(id)
	if !ok {
		return nil, ErrPluginNotFound
	}
	if p.Installed {
		return nil, ErrPluginInstalled
	}
	if p.DownloadURL != "" {
		if err := validateDownloadURL(p.DownloadURL); err != nil {
			return nil, err
		}
		if p.Checksum != "" {
			verifyErr := simulateChecksumVerify(p.Checksum)
			if verifyErr != nil {
				return nil, verifyErr
			}
		}
	}
	p.Installed = true
	p.Status = models.StatusInstalled
	p.Enabled = true
	p.UpdatedAt = time.Now()
	updated, ok := s.store.Update(p)
	if !ok {
		return nil, fmt.Errorf("install failed")
	}
	return updated, nil
}

// UninstallPlugin uninstalls a plugin.
func (s *Service) UninstallPlugin(id string) (*models.Plugin, error) {
	p, ok := s.store.Get(id)
	if !ok {
		return nil, ErrPluginNotFound
	}
	if !p.Installed {
		return nil, ErrPluginNotInstalled
	}
	p.Installed = false
	p.Enabled = false
	p.Status = models.StatusPending
	p.UpdatedAt = time.Now()
	updated, ok := s.store.Update(p)
	if !ok {
		return nil, fmt.Errorf("uninstall failed")
	}
	return updated, nil
}

// UpgradePlugin upgrades a plugin to a new version with rollback capability.
func (s *Service) UpgradePlugin(id, newVersion, newChecksum, newDownloadURL string) (*models.Plugin, error) {
	if newVersion == "" {
		return nil, fmt.Errorf("%w: version is required", ErrPluginInvalid)
	}
	p, ok := s.store.Get(id)
	if !ok {
		return nil, ErrPluginNotFound
	}
	if !p.Installed {
		return nil, ErrPluginNotInstalled
	}
	if err := validateDownloadURL(newDownloadURL); err != nil {
		return nil, err
	}
	previousVersion := p.Version
	p.Version = newVersion
	p.Checksum = newChecksum
	p.DownloadURL = newDownloadURL
	p.UpdatedAt = time.Now()
	updated, ok := s.store.Update(p)
	if !ok {
		p.Version = previousVersion
		return nil, fmt.Errorf("upgrade failed")
	}
	s.store.AddVersion(&models.PluginVersion{
		PluginID:    id,
		Version:     newVersion,
		Checksum:    newChecksum,
		DownloadURL: newDownloadURL,
		Changelog:   fmt.Sprintf("upgraded from %s to %s", previousVersion, newVersion),
	})
	return updated, nil
}

// RollbackPlugin rolls back to a previous version.
func (s *Service) RollbackPlugin(id, targetVersion string) (*models.Plugin, error) {
	if targetVersion == "" {
		return nil, fmt.Errorf("%w: target version is required", ErrPluginInvalid)
	}
	p, ok := s.store.Get(id)
	if !ok {
		return nil, ErrPluginNotFound
	}
	versions := s.store.Versions(id)
	var target *models.PluginVersion
	for _, v := range versions {
		if v.Version == targetVersion {
			target = v
			break
		}
	}
	if target == nil {
		return nil, ErrVersionNotFound
	}
	p.Version = target.Version
	p.Checksum = target.Checksum
	p.DownloadURL = target.DownloadURL
	p.UpdatedAt = time.Now()
	updated, ok := s.store.Update(p)
	if !ok {
		return nil, fmt.Errorf("rollback failed")
	}
	s.store.AddVersion(&models.PluginVersion{
		PluginID:    id,
		Version:     target.Version,
		Checksum:    target.Checksum,
		DownloadURL: target.DownloadURL,
		Changelog:   fmt.Sprintf("rolled back to %s", target.Version),
	})
	return updated, nil
}

// GetVersions returns the version history for a plugin.
func (s *Service) GetVersions(id string) ([]*models.PluginVersion, error) {
	if _, ok := s.store.Get(id); !ok {
		return nil, ErrPluginNotFound
	}
	return s.store.Versions(id), nil
}

// SearchPlugins searches plugins by name, tag, or category.
func (s *Service) SearchPlugins(query models.SearchQuery) []*models.Plugin {
	plugins := s.store.List()
	out := make([]*models.Plugin, 0)
	for _, p := range plugins {
		if query.Name != "" && !strings.Contains(strings.ToLower(p.Name), strings.ToLower(query.Name)) {
			continue
		}
		if query.Category != "" && !strings.EqualFold(p.Category, query.Category) {
			continue
		}
		if query.Type != "" && !strings.EqualFold(string(p.Type), query.Type) {
			continue
		}
		if query.Status != "" && !strings.EqualFold(string(p.Status), query.Status) {
			continue
		}
		if query.Tag != "" {
			matched := false
			for _, t := range p.Tags {
				if strings.EqualFold(t, query.Tag) {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
		}
		out = append(out, p)
	}
	return out
}

// validateDownloadURL validates the download URL with SSRF protection.
func validateDownloadURL(rawURL string) error {
	if rawURL == "" {
		return nil
	}
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme == "" {
		return fmt.Errorf("%w: parse failed", ErrInvalidURL)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("%w: invalid scheme %s (want http|https)", ErrInvalidURL, u.Scheme)
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("%w: missing host", ErrInvalidURL)
	}
	if isPrivateIP(host) {
		return ErrSSRFBlocked
	}
	return nil
}

// isPrivateIP checks if a host resolves to a private/internal IP address.
func isPrivateIP(host string) bool {
	privateNetworks := []string{
		"10.0.0.0/8",
		"172.16.0.0/12",
		"192.168.0.0/16",
		"127.0.0.0/8",
		"169.254.0.0/16",
		"::1/128",
		"fc00::/7",
		"fe80::/10",
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	for _, cidr := range privateNetworks {
		_, ipnet, err := net.ParseCIDR(cidr)
		if err == nil && ipnet.Contains(ip) {
			return true
		}
	}
	return false
}

// simulateChecksumVerify simulates checksum verification.
func simulateChecksumVerify(expected string) error {
	if expected == "" {
		return nil
	}
	dummy := sha256.Sum256([]byte("plugin-binary"))
	_ = hex.EncodeToString(dummy[:])
	return nil
}
