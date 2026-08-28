package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"

	"github.com/Levango7/OpsMesh/services/tf-provider/internal/client"
)

// New creates a new OpsMesh provider.
func New() *schema.Provider {
	return &schema.Provider{
		Schema: map[string]*schema.Schema{
			"api_url": {
				Type:        schema.TypeString,
				Required:    true,
				DefaultFunc: schema.EnvDefaultFunc("OPSMESH_API_URL", ""),
				Description: "The URL of the OpsMesh API server",
			},
			"token": {
				Type:        schema.TypeString,
				Optional:    true,
				Sensitive:   true,
				DefaultFunc: schema.EnvDefaultFunc("OPSMESH_TOKEN", ""),
				Description: "The authentication token for the OpsMesh API",
			},
		},
		ResourcesMap: map[string]*schema.Resource{
			"opsmesh_device":       resourceDevice(),
			"opsmesh_task":         resourceTask(),
			"opsmesh_alert_rule":   resourceAlertRule(),
			"opsmesh_deployment":   resourceDeployment(),
		},
		DataSourcesMap: map[string]*schema.Resource{
			"opsmesh_device": dataSourceDevice(),
			"opsmesh_alerts": dataSourceAlerts(),
		},
		ConfigureContextFunc: configure,
	}
}

func configure(_ context.Context, d *schema.ResourceData) (interface{}, diag.Diagnostics) {
	apiURL := d.Get("api_url").(string)
	token := d.Get("token").(string)

	if apiURL == "" {
		return nil, diag.FromErr(fmt.Errorf("api_url is required"))
	}

	c := client.NewClient(apiURL, token)
	return c, nil
}
