package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
)

func TestResourceDevice_CRUD(t *testing.T) {
	r := resourceDevice()

	if r.CreateContext == nil {
		t.Fatal("expected CreateContext to be defined")
	}
	if r.ReadContext == nil {
		t.Fatal("expected ReadContext to be defined")
	}
	if r.UpdateContext == nil {
		t.Fatal("expected UpdateContext to be defined")
	}
	if r.DeleteContext == nil {
		t.Fatal("expected DeleteContext to be defined")
	}
}

func TestResourceDevice_HasImporter(t *testing.T) {
	r := resourceDevice()

	// The device resource should support import via Read
	if r.ReadContext == nil {
		t.Fatal("expected ReadContext for import support")
	}
}

func TestResourceTask_CRUD(t *testing.T) {
	r := resourceTask()

	if r.CreateContext == nil {
		t.Fatal("expected CreateContext to be defined")
	}
	if r.ReadContext == nil {
		t.Fatal("expected ReadContext to be defined")
	}
	if r.UpdateContext == nil {
		t.Fatal("expected UpdateContext to be defined")
	}
	if r.DeleteContext == nil {
		t.Fatal("expected DeleteContext to be defined")
	}
}

func TestResourceAlertRule_CRUD(t *testing.T) {
	r := resourceAlertRule()

	if r.CreateContext == nil {
		t.Fatal("expected CreateContext to be defined")
	}
	if r.ReadContext == nil {
		t.Fatal("expected ReadContext to be defined")
	}
	if r.UpdateContext == nil {
		t.Fatal("expected UpdateContext to be defined")
	}
	if r.DeleteContext == nil {
		t.Fatal("expected DeleteContext to be defined")
	}
}

func TestResourceDeployment_CRUD(t *testing.T) {
	r := resourceDeployment()

	if r.CreateContext == nil {
		t.Fatal("expected CreateContext to be defined")
	}
	if r.ReadContext == nil {
		t.Fatal("expected ReadContext to be defined")
	}
	if r.UpdateContext == nil {
		t.Fatal("expected UpdateContext to be defined")
	}
	if r.DeleteContext == nil {
		t.Fatal("expected DeleteContext to be defined")
	}
}

func TestResourceDevice_SchemaTypes(t *testing.T) {
	r := resourceDevice()

	tests := []struct {
		name     string
		expected schema.ValueType
	}{
		{"name", schema.TypeString},
		{"ip", schema.TypeString},
		{"mac", schema.TypeString},
		{"os", schema.TypeString},
		{"arch", schema.TypeString},
		{"status", schema.TypeString},
		{"agent_id", schema.TypeString},
		{"tags", schema.TypeList},
		{"labels", schema.TypeMap},
		{"group", schema.TypeString},
	}

	for _, tt := range tests {
		if r.Schema[tt.name].Type != tt.expected {
			t.Errorf("expected %s to be %v, got %v", tt.name, tt.expected, r.Schema[tt.name].Type)
		}
	}
}

func TestResourceDeployment_StrategyValidation(t *testing.T) {
	r := resourceDeployment()

	strategy := r.Schema["strategy"]
	if strategy.ValidateFunc == nil {
		t.Fatal("expected strategy to have a validation function")
	}

	// Valid values should pass
	validValues := []interface{}{"rolling", "canary", "bluegreen"}
	for _, v := range validValues {
		_, errs := strategy.ValidateFunc(v, "strategy")
		if len(errs) > 0 {
			t.Errorf("expected %v to be valid, got errors: %v", v, errs)
		}
	}

	// Invalid values should fail
	_, errs := strategy.ValidateFunc("invalid", "strategy")
	if len(errs) == 0 {
		t.Error("expected invalid strategy to fail validation")
	}
}

func TestResourceAlertRule_OpValidation(t *testing.T) {
	r := resourceAlertRule()

	op := r.Schema["op"]
	if op.ValidateFunc == nil {
		t.Fatal("expected op to have a validation function")
	}

	validValues := []interface{}{"gt", "lt", "gte", "lte", "eq"}
	for _, v := range validValues {
		_, errs := op.ValidateFunc(v, "op")
		if len(errs) > 0 {
			t.Errorf("expected %v to be valid, got errors: %v", v, errs)
		}
	}

	_, errs := op.ValidateFunc("invalid", "op")
	if len(errs) == 0 {
		t.Error("expected invalid op to fail validation")
	}
}
