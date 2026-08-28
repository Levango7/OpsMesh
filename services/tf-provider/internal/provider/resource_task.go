package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"

	"github.com/Levango7/OpsMesh/services/tf-provider/internal/client"
)

func resourceTask() *schema.Resource {
	return &schema.Resource{
		CreateContext: resourceTaskCreate,
		ReadContext:   resourceTaskRead,
		UpdateContext: resourceTaskUpdate,
		DeleteContext: resourceTaskDelete,
		Schema: map[string]*schema.Schema{
			"agent_id": {
				Type:        schema.TypeString,
				Required:    true,
				ForceNew:    true,
				Description: "The agent ID to execute the task",
			},
			"type": {
				Type:        schema.TypeString,
				Required:    true,
				Description: "The type of task (shell, service, file)",
			},
			"command": {
				Type:        schema.TypeString,
				Optional:    true,
				Description: "The command to execute",
			},
			"content": {
				Type:        schema.TypeString,
				Optional:    true,
				Description: "The content for file-based tasks",
			},
			"path": {
				Type:        schema.TypeString,
				Optional:    true,
				Description: "The path for file-based tasks",
			},
			"status": {
				Type:        schema.TypeString,
				Computed:    true,
				Description: "The current status of the task",
			},
			"timeout": {
				Type:        schema.TypeInt,
				Optional:    true,
				Default:     300,
				Description: "Task timeout in seconds",
			},
			"schedule": {
				Type:        schema.TypeString,
				Optional:    true,
				Description: "Cron expression for scheduled tasks",
			},
			"parent_id": {
				Type:        schema.TypeString,
				Optional:    true,
				ForceNew:    true,
				Description: "Parent task ID for dependent tasks",
			},
			"dependencies": {
				Type:        schema.TypeList,
				Optional:    true,
				Elem:        &schema.Schema{Type: schema.TypeString},
				Description: "Task IDs this task depends on",
			},
		},
	}
}

func resourceTaskCreate(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
	c := m.(*client.Client)

	task := &client.Task{
		AgentID:  d.Get("agent_id").(string),
		Type:     d.Get("type").(string),
		Command:  d.Get("command").(string),
		Content:  d.Get("content").(string),
		Path:     d.Get("path").(string),
		Timeout:  d.Get("timeout").(int),
		Schedule: d.Get("schedule").(string),
		ParentID: d.Get("parent_id").(string),
	}

	if v, ok := d.GetOk("dependencies"); ok {
		for _, dep := range v.([]interface{}) {
			task.DependsOn = append(task.DependsOn, dep.(string))
		}
	}

	result, err := c.CreateTask(task)
	if err != nil {
		return diag.FromErr(err)
	}

	d.SetId(result.TaskID)

	return resourceTaskRead(ctx, d, m)
}

func resourceTaskRead(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
	c := m.(*client.Client)

	result, err := c.GetTask(d.Id())
	if err != nil {
		d.SetId("")
		return diag.FromErr(err)
	}

	d.Set("agent_id", result.AgentID)
	d.Set("type", result.Type)
	d.Set("command", result.Command)
	d.Set("content", result.Content)
	d.Set("path", result.Path)
	d.Set("status", result.Status)
	d.Set("timeout", result.Timeout)
	d.Set("schedule", result.Schedule)
	d.Set("parent_id", result.ParentID)
	d.Set("dependencies", result.DependsOn)

	return nil
}

func resourceTaskUpdate(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
	c := m.(*client.Client)

	task := &client.Task{
		TaskID:   d.Id(),
		AgentID:  d.Get("agent_id").(string),
		Type:     d.Get("type").(string),
		Command:  d.Get("command").(string),
		Content:  d.Get("content").(string),
		Path:     d.Get("path").(string),
		Timeout:  d.Get("timeout").(int),
		Schedule: d.Get("schedule").(string),
		ParentID: d.Get("parent_id").(string),
	}

	if v, ok := d.GetOk("dependencies"); ok {
		for _, dep := range v.([]interface{}) {
			task.DependsOn = append(task.DependsOn, dep.(string))
		}
	}

	_, err := c.UpdateTask(task)
	if err != nil {
		return diag.FromErr(err)
	}

	return resourceTaskRead(ctx, d, m)
}

func resourceTaskDelete(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
	c := m.(*client.Client)

	err := c.DeleteTask(d.Id())
	if err != nil {
		return diag.FromErr(err)
	}

	d.SetId("")
	return nil
}
