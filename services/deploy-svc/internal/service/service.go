package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Levango7/OpsMesh/services/deploy-svc/internal/models"
	"github.com/Levango7/OpsMesh/services/deploy-svc/internal/store"
)

// Errors returned by the service.
var (
	ErrDeploymentNotFound = errors.New("deployment not found")
	ErrDeploymentInvalid  = errors.New("deployment invalid")
	ErrTemplateNotFound   = errors.New("template not found")
	ErrTemplateInvalid    = errors.New("template invalid")
	ErrStrategyNotFound   = errors.New("strategy not found")
	ErrStrategyInvalid    = errors.New("strategy invalid")
	ErrCanaryNotFound     = errors.New("canary not found")
	ErrCanaryInvalid      = errors.New("canary invalid")
	ErrTenantMismatch     = errors.New("tenant mismatch")
)

// Service implements the deploy service business logic.
type Service struct {
	store store.Store
}

// NewService creates a new Service.
func NewService(s store.Store) *Service {
	return &Service{store: s}
}

// Deployment lifecycle

func (s *Service) CreateDeployment(ctx context.Context, tenantID, name, depType, repoURL, content, path string, targetIDs []string, strategy string, canaryWeight int, autoRollback bool, createdBy string) (*models.Deployment, error) {
	if !models.IsValidDeploymentType(depType) {
		return nil, ErrDeploymentInvalid
	}
	if strategy == "" {
		strategy = models.StrategyRolling
	}
	if !models.IsValidStrategy(strategy) {
		return nil, fmt.Errorf("%w: invalid strategy %s", ErrDeploymentInvalid, strategy)
	}

	d := &models.Deployment{
		TenantID:     tenantID,
		Name:         name,
		Type:         depType,
		RepoURL:      repoURL,
		Content:      content,
		Path:         path,
		TargetIDs:    targetIDs,
		Strategy:     strategy,
		CanaryWeight: canaryWeight,
		AutoRollback: autoRollback,
		CreatedBy:    createdBy,
	}
	return s.store.CreateDeployment(d)
}

func (s *Service) GetDeployment(ctx context.Context, id, tenantID string) (*models.Deployment, error) {
	d, err := s.store.GetDeployment(id, tenantID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, ErrDeploymentNotFound
		}
		if errors.Is(err, store.ErrTenantMismatch) {
			return nil, ErrTenantMismatch
		}
		return nil, err
	}
	return d, nil
}

func (s *Service) ListDeployments(ctx context.Context, tenantID, status string) ([]*models.Deployment, error) {
	return s.store.ListDeployments(tenantID, status)
}

func (s *Service) RollbackDeployment(ctx context.Context, id, tenantID string) (*models.Deployment, error) {
	d, err := s.store.GetDeployment(id, tenantID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, ErrDeploymentNotFound
		}
		if errors.Is(err, store.ErrTenantMismatch) {
			return nil, ErrTenantMismatch
		}
		return nil, err
	}
	if models.IsTerminalStatus(d.Status) {
		return nil, fmt.Errorf("%w: cannot rollback from status %s", ErrDeploymentInvalid, d.Status)
	}
	d.Status = models.DeploymentStatusRollback
	d.UpdatedAt = time.Now()
	if err := s.store.UpdateDeployment(d); err != nil {
		return nil, err
	}
	return d, nil
}

func (s *Service) CancelDeployment(ctx context.Context, id, tenantID string) (*models.Deployment, error) {
	d, err := s.store.GetDeployment(id, tenantID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, ErrDeploymentNotFound
		}
		if errors.Is(err, store.ErrTenantMismatch) {
			return nil, ErrTenantMismatch
		}
		return nil, err
	}
	if models.IsTerminalStatus(d.Status) {
		return nil, fmt.Errorf("%w: cannot cancel from status %s", ErrDeploymentInvalid, d.Status)
	}
	d.Status = models.DeploymentStatusCancelled
	d.UpdatedAt = time.Now()
	if err := s.store.UpdateDeployment(d); err != nil {
		return nil, err
	}
	return d, nil
}

func (s *Service) GetDeploymentStatus(ctx context.Context, id, tenantID string) (*models.Deployment, error) {
	d, err := s.store.GetDeployment(id, tenantID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, ErrDeploymentNotFound
		}
		if errors.Is(err, store.ErrTenantMismatch) {
			return nil, ErrTenantMismatch
		}
		return nil, err
	}
	return d, nil
}

// Template management

func (s *Service) CreateTemplate(ctx context.Context, tenantID, name, description, depType, repoURL, content, path string, parameters map[string]string, createdBy string) (*models.Template, error) {
	t := &models.Template{
		TenantID:    tenantID,
		Name:        name,
		Description: description,
		Type:        depType,
		RepoURL:     repoURL,
		Content:     content,
		Path:        path,
		Parameters:  parameters,
		CreatedBy:   createdBy,
	}
	return s.store.CreateTemplate(t)
}

func (s *Service) GetTemplate(ctx context.Context, id, tenantID string) (*models.Template, error) {
	t, err := s.store.GetTemplate(id, tenantID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, ErrTemplateNotFound
		}
		if errors.Is(err, store.ErrTenantMismatch) {
			return nil, ErrTenantMismatch
		}
		return nil, err
	}
	return t, nil
}

func (s *Service) UpdateTemplate(ctx context.Context, t *models.Template) (*models.Template, error) {
	if t == nil {
		return nil, ErrTemplateInvalid
	}
	if err := s.store.UpdateTemplate(t); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, ErrTemplateNotFound
		}
		if errors.Is(err, store.ErrTenantMismatch) {
			return nil, ErrTenantMismatch
		}
		return nil, err
	}
	return t, nil
}

func (s *Service) DeleteTemplate(ctx context.Context, id, tenantID string) error {
	err := s.store.DeleteTemplate(id, tenantID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return ErrTemplateNotFound
		}
		if errors.Is(err, store.ErrTenantMismatch) {
			return ErrTenantMismatch
		}
		return err
	}
	return nil
}

func (s *Service) ListTemplates(ctx context.Context, tenantID string) ([]*models.Template, error) {
	return s.store.ListTemplates(tenantID)
}

// Strategy management

func (s *Service) CreateStrategy(ctx context.Context, tenantID, name, description, strategyType string, canaryWeight, maxUnavailable, maxSurge int, autoRollback bool, timeoutSeconds int, createdBy string) (*models.Strategy, error) {
	if !models.IsValidStrategy(strategyType) {
		return nil, ErrStrategyInvalid
	}
	st := &models.Strategy{
		TenantID:       tenantID,
		Name:           name,
		Description:    description,
		Type:           strategyType,
		CanaryWeight:   canaryWeight,
		MaxUnavailable: maxUnavailable,
		MaxSurge:       maxSurge,
		AutoRollback:   autoRollback,
		TimeoutSeconds: timeoutSeconds,
		CreatedBy:      createdBy,
	}
	return s.store.CreateStrategy(st)
}

func (s *Service) GetStrategy(ctx context.Context, id, tenantID string) (*models.Strategy, error) {
	st, err := s.store.GetStrategy(id, tenantID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, ErrStrategyNotFound
		}
		if errors.Is(err, store.ErrTenantMismatch) {
			return nil, ErrTenantMismatch
		}
		return nil, err
	}
	return st, nil
}

func (s *Service) UpdateStrategy(ctx context.Context, st *models.Strategy) (*models.Strategy, error) {
	if st == nil {
		return nil, ErrStrategyInvalid
	}
	if err := s.store.UpdateStrategy(st); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, ErrStrategyNotFound
		}
		if errors.Is(err, store.ErrTenantMismatch) {
			return nil, ErrTenantMismatch
		}
		return nil, err
	}
	return st, nil
}

func (s *Service) DeleteStrategy(ctx context.Context, id, tenantID string) error {
	err := s.store.DeleteStrategy(id, tenantID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return ErrStrategyNotFound
		}
		if errors.Is(err, store.ErrTenantMismatch) {
			return ErrTenantMismatch
		}
		return err
	}
	return nil
}

func (s *Service) ListStrategies(ctx context.Context, tenantID string) ([]*models.Strategy, error) {
	return s.store.ListStrategies(tenantID)
}

// Canary management

func (s *Service) StartCanary(ctx context.Context, tenantID, deploymentID, name string, weight int, createdBy string) (*models.Canary, error) {
	if weight <= 0 || weight > 100 {
		return nil, fmt.Errorf("%w: weight must be in [1,100]", ErrCanaryInvalid)
	}
	c := &models.Canary{
		TenantID:     tenantID,
		DeploymentID: deploymentID,
		Name:         name,
		Weight:       weight,
		CreatedBy:    createdBy,
	}
	return s.store.CreateCanary(c)
}

func (s *Service) GetCanaryStatus(ctx context.Context, canaryID, tenantID string) (*models.Canary, error) {
	c, err := s.store.GetCanary(canaryID, tenantID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, ErrCanaryNotFound
		}
		if errors.Is(err, store.ErrTenantMismatch) {
			return nil, ErrTenantMismatch
		}
		return nil, err
	}
	return c, nil
}

func (s *Service) PromoteCanary(ctx context.Context, canaryID, tenantID string) (*models.Canary, error) {
	c, err := s.store.GetCanary(canaryID, tenantID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, ErrCanaryNotFound
		}
		if errors.Is(err, store.ErrTenantMismatch) {
			return nil, ErrTenantMismatch
		}
		return nil, err
	}
	if c.Status != models.CanaryStatusRunning && c.Status != models.CanaryStatusAnalyzing {
		return nil, fmt.Errorf("%w: cannot promote from status %s", ErrCanaryInvalid, c.Status)
	}
	c.Status = models.CanaryStatusPromoted
	c.UpdatedAt = time.Now()
	if err := s.store.UpdateCanary(c); err != nil {
		return nil, err
	}
	return c, nil
}

func (s *Service) RollbackCanary(ctx context.Context, canaryID, tenantID string) (*models.Canary, error) {
	c, err := s.store.GetCanary(canaryID, tenantID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, ErrCanaryNotFound
		}
		if errors.Is(err, store.ErrTenantMismatch) {
			return nil, ErrTenantMismatch
		}
		return nil, err
	}
	if c.Status == models.CanaryStatusPromoted || c.Status == models.CanaryStatusRollback {
		return nil, fmt.Errorf("%w: cannot rollback from status %s", ErrCanaryInvalid, c.Status)
	}
	c.Status = models.CanaryStatusRollback
	c.UpdatedAt = time.Now()
	if err := s.store.UpdateCanary(c); err != nil {
		return nil, err
	}
	return c, nil
}

func (s *Service) ListCanaries(ctx context.Context, tenantID, status string) ([]*models.Canary, error) {
	return s.store.ListCanaries(tenantID, status)
}
