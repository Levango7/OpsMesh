package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/validation"

	"github.com/Levango7/OpsMesh/services/tf-provider/internal/client"
)

func resourceAlertRule() *schema.Resource {
	return &schema.Resource{
		CreateContext: resourceAlertRuleCreate,
		ReadContext:   resourceAlertRuleRead,
		UpdateContext: resourceAlertRuleUpdate,
		DeleteContext: resourceAlertRuleDelete,
		Schema: map[string]*schema.Schema{
			"name": {
				Type:        schema.TypeString,
				Required:    true,
				Description: "The name of the alert rule",
			},
			"metric": {
				Type:        schema.TypeString,
				Required:    true,
				Description: "The metric to monitor",
			},
			"op": {
				Type:         schema.TypeString,
				Required:     true,
				Description:  "The comparison operator (gt, lt, gte, lte, eq)",
				ValidateFunc: validation.StringInSlice([]string{"gt", "lt", "gte", "lte", "eq"}, false),
			},
			"threshold": {
				Type:        schema.TypeFloat,
				Required:    true,
				Description: "The threshold value for the alert",
			},
			"duration": {
				Type:        schema.TypeInt,
				Optional:    true,
				Default:     60,
				Description: "Duration in seconds before alert fires",
			},
			"severity": {
				Type:         schema.TypeString,
				Optional:     true,
				Default:      "warning",
				Description:  "The severity level (info, warning, critical)",
				ValidateFunc: validation.StringInSlice([]string{"info", "warning", "critical"}, false),
			},
			"channels": {
				Type:        schema.TypeList,
				Optional:    true,
				Elem:        &schema.Schema{Type: schema.TypeString},
				Description: "Notification channels",
			},
			"enabled": {
				Type:        schema.TypeBool,
				Optional:    true,
				Default:     true,
				Description: "Whether the rule is enabled",
			},
		},
	}
}

func resourceAlertRuleCreate(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
	c := m.(*client.Client)

	rule := &client.AlertRule{
		Name:      d.Get("name").(string),
		Metric:    d.Get("metric").(string),
		Op:        d.Get("op").(string),
		Threshold: d.Get("threshold").(float64),
		Duration:  d.Get("duration").(int),
		Severity:  d.Get("severity").(string),
		Enabled:   d.Get("enabled").(bool),
	}

	if v, ok := d.GetOk("channels"); ok {
		for _, ch := range v.([]interface{}) {
			rule.Channels = append(rule.Channels, ch.(string))
		}
	}

	result, err := c.CreateAlertRule(rule)
	if err != nil {
		return diag.FromErr(err)
	}

	d.SetId(result.ID)

	return resourceAlertRuleRead(ctx, d, m)
}

func resourceAlertRuleRead(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
	c := m.(*client.Client)

	result, err := c.GetAlertRule(d.Id())
	if err != nil {
		d.SetId("")
		return diag.FromErr(err)
	}

	d.Set("name", result.Name)
	d.Set("metric", result.Metric)
	d.Set("op", result.Op)
	d.Set("threshold", result.Threshold)
	d.Set("duration", result.Duration)
	d.Set("severity", result.Severity)
	d.Set("channels", result.Channels)
	d.Set("enabled", result.Enabled)

	return nil
}

func resourceAlertRuleUpdate(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
	c := m.(*client.Client)

	rule := &client.AlertRule{
		ID:        d.Id(),
		Name:      d.Get("name").(string),
		Metric:    d.Get("metric").(string),
		Op:        d.Get("op").(string),
		Threshold: d.Get("threshold").(float64),
		Duration:  d.Get("duration").(int),
		Severity:  d.Get("severity").(string),
		Enabled:   d.Get("enabled").(bool),
	}

	if v, ok := d.GetOk("channels"); ok {
		for _, ch := range v.([]interface{}) {
			rule.Channels = append(rule.Channels, ch.(string))
		}
	}

	_, err := c.UpdateAlertRule(rule)
	if err != nil {
		return diag.FromErr(err)
	}

	return resourceAlertRuleRead(ctx, d, m)
}

func resourceAlertRuleDelete(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
	c := m.(*client.Client)

	err := c.DeleteAlertRule(d.Id())
	if err != nil {
		return diag.FromErr(err)
	}

	d.SetId("")
	return nil
}
