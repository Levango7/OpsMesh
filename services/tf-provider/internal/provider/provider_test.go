package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
)

func TestProvider(t *testing.T) {
	p := New()
	if err := p.InternalValidate(); err != nil {
		t.Fatalf("provider internal validation failed: %s", err)
	}
}

func TestProvider_Schema(t *testing.T) {
	p := New()

	// Validate required fields
	if p.Schema["api_url"] == nil {
		t.Fatal("api_url schema is missing")
	}
	if !p.Schema["api_url"].Required {
		t.Fatal("api_url should be required")
	}

	if p.Schema["token"] == nil {
		t.Fatal("token schema is missing")
	}
	if !p.Schema["token"].Optional {
		t.Fatal("token should be optional")
	}
	if !p.Schema["token"].Sensitive {
		t.Fatal("token should be sensitive")
	}
}

func TestProvider_Resources(t *testing.T) {
	p := New()

	expectedResources := []string{
		"opsmesh_device",
		"opsmesh_task",
		"opsmesh_alert_rule",
		"opsmesh_deployment",
	}

	for _, name := range expectedResources {
		if _, ok := p.ResourcesMap[name]; !ok {
			t.Fatalf("expected resource %q to be registered", name)
		}
	}
}

func TestProvider_DataSources(t *testing.T) {
	p := New()

	expectedDataSources := []string{
		"opsmesh_device",
		"opsmesh_alerts",
	}

	for _, name := range expectedDataSources {
		if _, ok := p.DataSourcesMap[name]; !ok {
			t.Fatalf("expected data source %q to be registered", name)
		}
	}
}

func TestProvider_ConfigureWithoutAPIUrl(t *testing.T) {
	p := New()
	d := schema.TestResourceDataRaw(t, p.Schema, map[string]interface{}{
		"api_url": "",
		"token":   "",
	})

	_, diags := p.ConfigureContextFunc(nil, d)
	if !diags.HasError() {
		t.Fatal("expected error when api_url is empty")
	}
}

func TestResourceDevice_Schema(t *testing.T) {
	r := resourceDevice()

	requiredFields := []string{"name"}
	for _, field := range requiredFields {
		if r.Schema[field] == nil {
			t.Fatalf("expected field %q in device schema", field)
		}
		if !r.Schema[field].Required {
			t.Fatalf("expected field %q to be required", field)
		}
	}

	optionalFields := []string{"ip", "mac", "os", "arch", "agent_id", "tags", "labels", "group"}
	for _, field := range optionalFields {
		if r.Schema[field] == nil {
			t.Fatalf("expected field %q in device schema", field)
		}
	}

	computedFields := []string{"status"}
	for _, field := range computedFields {
		if !r.Schema[field].Computed {
			t.Fatalf("expected field %q to be computed", field)
		}
	}
}

func TestResourceTask_Schema(t *testing.T) {
	r := resourceTask()

	requiredFields := []string{"agent_id", "type"}
	for _, field := range requiredFields {
		if r.Schema[field] == nil {
			t.Fatalf("expected field %q in task schema", field)
		}
		if !r.Schema[field].Required {
			t.Fatalf("expected field %q to be required", field)
		}
	}
}

func TestResourceAlertRule_Schema(t *testing.T) {
	r := resourceAlertRule()

	requiredFields := []string{"name", "metric", "op", "threshold"}
	for _, field := range requiredFields {
		if r.Schema[field] == nil {
			t.Fatalf("expected field %q in alert rule schema", field)
		}
		if !r.Schema[field].Required {
			t.Fatalf("expected field %q to be required", field)
		}
	}
}

func TestResourceDeployment_Schema(t *testing.T) {
	r := resourceDeployment()

	requiredFields := []string{"name", "type"}
	for _, field := range requiredFields {
		if r.Schema[field] == nil {
			t.Fatalf("expected field %q in deployment schema", field)
		}
		if !r.Schema[field].Required {
			t.Fatalf("expected field %q to be required", field)
		}
	}
}

func TestDataSourceDevice_Schema(t *testing.T) {
	d := dataSourceDevice()

	if d.Schema["id"] == nil {
		t.Fatal("expected id field in device data source schema")
	}
	if !d.Schema["id"].Required {
		t.Fatal("expected id to be required in device data source")
	}

	computedFields := []string{"name", "ip", "mac", "os", "arch", "status", "agent_id", "tags", "labels", "group"}
	for _, field := range computedFields {
		if !d.Schema[field].Computed {
			t.Fatalf("expected field %q to be computed in device data source", field)
		}
	}
}

func TestDataSourceAlerts_Schema(t *testing.T) {
	d := dataSourceAlerts()

	if d.Schema["alerts"] == nil {
		t.Fatal("expected alerts field in alerts data source schema")
	}
	if !d.Schema["alerts"].Computed {
		t.Fatal("expected alerts to be computed in alerts data source")
	}
}
