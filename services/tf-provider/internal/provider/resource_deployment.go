package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/validation"

	"github.com/Levango7/OpsMesh/services/tf-provider/internal/client"
)

func resourceDeployment() *schema.Resource {
	return &schema.Resource{
		CreateContext: resourceDeploymentCreate,
		ReadContext:   resourceDeploymentRead,
		UpdateContext: resourceDeploymentUpdate,
		DeleteContext: resourceDeploymentDelete,
		Schema: map[string]*schema.Schema{
			"name": {
				Type:        schema.TypeString,
				Required:    true,
				Description: "The name of the deployment",
			},
			"type": {
				Type:         schema.TypeString,
				Required:     true,
				Description:  "The deployment type (script, file, k8s)",
				ValidateFunc: validation.StringInSlice([]string{"script", "file", "k8s"}, false),
			},
			"repo_url": {
				Type:        schema.TypeString,
				Optional:    true,
				Description: "The repository URL for the deployment",
			},
			"content": {
				Type:        schema.TypeString,
				Optional:    true,
				Description: "The deployment content/script",
			},
			"path": {
				Type:        schema.TypeString,
				Optional:    true,
				Description: "The deployment path",
			},
			"target_ids": {
				Type:        schema.TypeList,
				Optional:    true,
				Elem:        &schema.Schema{Type: schema.TypeString},
				Description: "Target device IDs for the deployment",
			},
			"status": {
				Type:        schema.TypeString,
				Computed:    true,
				Description: "The current status of the deployment",
			},
			"strategy": {
				Type:         schema.TypeString,
				Optional:     true,
				Default:      "rolling",
				Description:  "The deployment strategy (rolling, canary, bluegreen)",
				ValidateFunc: validation.StringInSlice([]string{"rolling", "canary", "bluegreen"}, false),
			},
			"canary_weight": {
				Type:        schema.TypeInt,
				Optional:    true,
				Default:     10,
				Description: "Canary weight percentage",
			},
			"auto_rollback": {
				Type:        schema.TypeBool,
				Optional:    true,
				Default:     true,
				Description: "Whether to auto-rollback on failure",
			},
			"created_by": {
				Type:        schema.TypeString,
				Optional:    true,
				Description: "The creator of the deployment",
			},
			"error_message": {
				Type:        schema.TypeString,
				Computed:    true,
				Description: "Error message if deployment failed",
			},
		},
	}
}

func resourceDeploymentCreate(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
	c := m.(*client.Client)

	deployment := &client.Deployment{
		Name:         d.Get("name").(string),
		Type:         d.Get("type").(string),
		RepoURL:      d.Get("repo_url").(string),
		Content:      d.Get("content").(string),
		Path:         d.Get("path").(string),
		Strategy:     d.Get("strategy").(string),
		CanaryWeight: d.Get("canary_weight").(int),
		AutoRollback: d.Get("auto_rollback").(bool),
		CreatedBy:    d.Get("created_by").(string),
	}

	if v, ok := d.GetOk("target_ids"); ok {
		for _, t := range v.([]interface{}) {
			deployment.TargetIDs = append(deployment.TargetIDs, t.(string))
		}
	}

	result, err := c.CreateDeployment(deployment)
	if err != nil {
		return diag.FromErr(err)
	}

	d.SetId(result.ID)

	return resourceDeploymentRead(ctx, d, m)
}

func resourceDeploymentRead(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
	c := m.(*client.Client)

	result, err := c.GetDeployment(d.Id())
	if err != nil {
		d.SetId("")
		return diag.FromErr(err)
	}

	d.Set("name", result.Name)
	d.Set("type", result.Type)
	d.Set("repo_url", result.RepoURL)
	d.Set("content", result.Content)
	d.Set("path", result.Path)
	d.Set("target_ids", result.TargetIDs)
	d.Set("status", result.Status)
	d.Set("strategy", result.Strategy)
	d.Set("canary_weight", result.CanaryWeight)
	d.Set("auto_rollback", result.AutoRollback)
	d.Set("created_by", result.CreatedBy)
	d.Set("error_message", result.ErrorMessage)

	return nil
}

func resourceDeploymentUpdate(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
	c := m.(*client.Client)

	deployment := &client.Deployment{
		ID:           d.Id(),
		Name:         d.Get("name").(string),
		Type:         d.Get("type").(string),
		RepoURL:      d.Get("repo_url").(string),
		Content:      d.Get("content").(string),
		Path:         d.Get("path").(string),
		Strategy:     d.Get("strategy").(string),
		CanaryWeight: d.Get("canary_weight").(int),
		AutoRollback: d.Get("auto_rollback").(bool),
		CreatedBy:    d.Get("created_by").(string),
	}

	if v, ok := d.GetOk("target_ids"); ok {
		for _, t := range v.([]interface{}) {
			deployment.TargetIDs = append(deployment.TargetIDs, t.(string))
		}
	}

	_, err := c.UpdateDeployment(deployment)
	if err != nil {
		return diag.FromErr(err)
	}

	return resourceDeploymentRead(ctx, d, m)
}

func resourceDeploymentDelete(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
	c := m.(*client.Client)

	err := c.DeleteDeployment(d.Id())
	if err != nil {
		return diag.FromErr(err)
	}

	d.SetId("")
	return nil
}
