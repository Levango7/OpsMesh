package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"

	"github.com/Levango7/OpsMesh/services/tf-provider/internal/client"
)

func dataSourceAlerts() *schema.Resource {
	return &schema.Resource{
		ReadContext: dataSourceAlertsRead,
		Schema: map[string]*schema.Schema{
			"alerts": {
				Type:        schema.TypeList,
				Computed:    true,
				Description: "List of alerts",
				Elem: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"id": {
							Type:        schema.TypeString,
							Computed:    true,
							Description: "Alert ID",
						},
						"rule_id": {
							Type:        schema.TypeString,
							Computed:    true,
							Description: "Rule ID that triggered the alert",
						},
						"severity": {
							Type:        schema.TypeString,
							Computed:    true,
							Description: "Alert severity",
						},
						"message": {
							Type:        schema.TypeString,
							Computed:    true,
							Description: "Alert message",
						},
						"status": {
							Type:        schema.TypeString,
							Computed:    true,
							Description: "Alert status",
						},
					},
				},
			},
		},
	}
}

func dataSourceAlertsRead(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
	c := m.(*client.Client)

	alerts, err := c.ListAlerts()
	if err != nil {
		return diag.FromErr(err)
	}

	var alertsList []map[string]interface{}
	for _, a := range alerts {
		alertsList = append(alertsList, map[string]interface{}{
			"id":       a.ID,
			"rule_id":  a.RuleID,
			"severity": a.Severity,
			"message":  a.Message,
			"status":   a.Status,
		})
	}

	d.SetId("opsmesh_alerts")
	d.Set("alerts", alertsList)

	return nil
}
