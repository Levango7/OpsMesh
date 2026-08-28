package cloud

import (
	"testing"
)

func TestNewProviderAWS(t *testing.T) {
	p, err := NewProvider(ProviderAWS)
	if err != nil {
		t.Fatalf("NewProvider(aws) failed: %v", err)
	}
	if p.ProviderName() != ProviderAWS {
		t.Fatalf("expected provider name %s, got %s", ProviderAWS, p.ProviderName())
	}
}

func TestNewProviderHuawei(t *testing.T) {
	p, err := NewProvider(ProviderHuawei)
	if err != nil {
		t.Fatalf("NewProvider(huawei) failed: %v", err)
	}
	if p.ProviderName() != ProviderHuawei {
		t.Fatalf("expected provider name %s, got %s", ProviderHuawei, p.ProviderName())
	}
}

func TestNewProviderAli(t *testing.T) {
	p, err := NewProvider(ProviderAli)
	if err != nil {
		t.Fatalf("NewProvider(ali) failed: %v", err)
	}
	if p.ProviderName() != ProviderAli {
		t.Fatalf("expected provider name %s, got %s", ProviderAli, p.ProviderName())
	}
}

func TestNewProviderOnPrem(t *testing.T) {
	p, err := NewProvider(ProviderOnPrem)
	if err != nil {
		t.Fatalf("NewProvider(onprem) failed: %v", err)
	}
	if p.ProviderName() != ProviderOnPrem {
		t.Fatalf("expected provider name %s, got %s", ProviderOnPrem, p.ProviderName())
	}
}

func TestNewProviderUnsupported(t *testing.T) {
	_, err := NewProvider("gcp")
	if err == nil {
		t.Fatal("expected error for unsupported provider, got nil")
	}
	if err != ErrProviderUnsupported {
		if err.Error() == "" {
			t.Fatal("expected ErrProviderUnsupported")
		}
	}
}

func TestAWSProviderValidate(t *testing.T) {
	p := NewAWSProvider("us-west-2")

	err := p.Validate(DeploymentConfig{DeploymentID: "dep-1", Name: "test", Type: "k8s"})
	if err != nil {
		t.Fatalf("expected valid config, got error: %v", err)
	}

	err = p.Validate(DeploymentConfig{Name: "test", Type: "k8s"})
	if err == nil {
		t.Fatal("expected error for missing deployment_id")
	}

	err = p.Validate(DeploymentConfig{DeploymentID: "dep-1", Type: "k8s"})
	if err == nil {
		t.Fatal("expected error for missing name")
	}

	err = p.Validate(DeploymentConfig{DeploymentID: "dep-1", Name: "test", Type: "invalid"})
	if err == nil {
		t.Fatal("expected error for invalid type")
	}
}

func TestAWSProviderDeployAndGetStatus(t *testing.T) {
	p := NewAWSProvider("eu-west-1")

	config := DeploymentConfig{
		DeploymentID: "dep-aws-1",
		Name:         "my-app",
		Type:         "k8s",
		Region:       "eu-west-1",
	}

	result, err := p.Deploy(config)
	if err != nil {
		t.Fatalf("Deploy failed: %v", err)
	}
	if result.Status != StatusRunning {
		t.Fatalf("expected status %s, got %s", StatusRunning, result.Status)
	}
	if result.Provider != ProviderAWS {
		t.Fatalf("expected provider %s, got %s", ProviderAWS, result.Provider)
	}

	status, err := p.GetStatus("dep-aws-1")
	if err != nil {
		t.Fatalf("GetStatus failed: %v", err)
	}
	if status.DeploymentID != "dep-aws-1" {
		t.Fatalf("expected deployment ID dep-aws-1, got %s", status.DeploymentID)
	}
}

func TestAWSProviderRollback(t *testing.T) {
	p := NewAWSProvider("")

	config := DeploymentConfig{DeploymentID: "dep-aws-2", Name: "rollback-test", Type: "script"}
	_, err := p.Deploy(config)
	if err != nil {
		t.Fatalf("Deploy failed: %v", err)
	}

	result, err := p.Rollback("dep-aws-2")
	if err != nil {
		t.Fatalf("Rollback failed: %v", err)
	}
	if result.Status != StatusRolledBack {
		t.Fatalf("expected status %s, got %s", StatusRolledBack, result.Status)
	}
}

func TestHuaweiProviderDeployAndRollback(t *testing.T) {
	p := NewHuaweiProvider("cn-southwest-2")

	config := DeploymentConfig{DeploymentID: "dep-hw-1", Name: "hw-app", Type: "file"}
	result, err := p.Deploy(config)
	if err != nil {
		t.Fatalf("Deploy failed: %v", err)
	}
	if result.Region != "cn-southwest-2" {
		t.Fatalf("expected region cn-southwest-2, got %s", result.Region)
	}

	rolledBack, err := p.Rollback("dep-hw-1")
	if err != nil {
		t.Fatalf("Rollback failed: %v", err)
	}
	if rolledBack.Status != StatusRolledBack {
		t.Fatalf("expected status %s, got %s", StatusRolledBack, rolledBack.Status)
	}
}

func TestAliProviderDeploy(t *testing.T) {
	p := NewAliProvider("cn-beijing")

	config := DeploymentConfig{DeploymentID: "dep-ali-1", Name: "ali-app", Type: "k8s"}
	result, err := p.Deploy(config)
	if err != nil {
		t.Fatalf("Deploy failed: %v", err)
	}
	if result.Provider != ProviderAli {
		t.Fatalf("expected provider %s, got %s", ProviderAli, result.Provider)
	}
	if result.Status != StatusRunning {
		t.Fatalf("expected status %s, got %s", StatusRunning, result.Status)
	}
}

func TestOnPremProviderValidateRequiresTargets(t *testing.T) {
	p := NewOnPremProvider()

	err := p.Validate(DeploymentConfig{DeploymentID: "dep-op-1", Name: "onprem-app", Type: "script"})
	if err == nil {
		t.Fatal("expected error for missing targets on on-premise deployment")
	}

	err = p.Validate(DeploymentConfig{DeploymentID: "dep-op-1", Name: "onprem-app", Type: "k8s", Targets: []string{"host1"}})
	if err == nil {
		t.Fatal("expected error for k8s type on on-premise provider")
	}
}

func TestOnPremProviderDeploy(t *testing.T) {
	p := NewOnPremProvider()

	config := DeploymentConfig{
		DeploymentID: "dep-op-2",
		Name:         "onprem-deploy",
		Type:         "script",
		Targets:      []string{"192.168.1.10", "192.168.1.11"},
	}
	result, err := p.Deploy(config)
	if err != nil {
		t.Fatalf("Deploy failed: %v", err)
	}
	if result.Region != "on-premise" {
		t.Fatalf("expected region on-premise, got %s", result.Region)
	}
}

func TestGetStatusNotFound(t *testing.T) {
	p := NewAWSProvider("")
	_, err := p.GetStatus("nonexistent")
	if err != ErrDeploymentNotFound {
		t.Fatalf("expected ErrDeploymentNotFound, got: %v", err)
	}
}

func TestRollbackNotFound(t *testing.T) {
	p := NewHuaweiProvider("")
	_, err := p.Rollback("nonexistent")
	if err != ErrDeploymentNotFound {
		t.Fatalf("expected ErrDeploymentNotFound, got: %v", err)
	}
}

func TestListProviders(t *testing.T) {
	providers := ListProviders()
	if len(providers) != 4 {
		t.Fatalf("expected 4 providers, got %d", len(providers))
	}
	expected := map[string]bool{ProviderAWS: true, ProviderHuawei: true, ProviderAli: true, ProviderOnPrem: true}
	for _, p := range providers {
		if !expected[p] {
			t.Fatalf("unexpected provider: %s", p)
		}
	}
}

func TestAWSProviderDefaultRegion(t *testing.T) {
	p := NewAWSProvider("")
	config := DeploymentConfig{DeploymentID: "dep-default", Name: "test", Type: "script"}
	result, err := p.Deploy(config)
	if err != nil {
		t.Fatalf("Deploy failed: %v", err)
	}
	if result.Region != "us-east-1" {
		t.Fatalf("expected default region us-east-1, got %s", result.Region)
	}
}
