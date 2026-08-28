package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"

	"github.com/Levango7/OpsMesh/services/tf-provider/internal/client"
)

func dataSourceDevice() *schema.Resource {
	return &schema.Resource{
		ReadContext: dataSourceDeviceRead,
		Schema: map[string]*schema.Schema{
			"id": {
				Type:        schema.TypeString,
				Required:    true,
				Description: "The ID of the device to look up",
			},
			"name": {
				Type:        schema.TypeString,
				Computed:    true,
				Description: "The name of the device",
			},
			"ip": {
				Type:        schema.TypeString,
				Computed:    true,
				Description: "The IP address of the device",
			},
			"mac": {
				Type:        schema.TypeString,
				Computed:    true,
				Description: "The MAC address of the device",
			},
			"os": {
				Type:        schema.TypeString,
				Computed:    true,
				Description: "The operating system of the device",
			},
			"arch": {
				Type:        schema.TypeString,
				Computed:    true,
				Description: "The architecture of the device",
			},
			"status": {
				Type:        schema.TypeString,
				Computed:    true,
				Description: "The current status of the device",
			},
			"agent_id": {
				Type:        schema.TypeString,
				Computed:    true,
				Description: "The agent ID associated with the device",
			},
			"tags": {
				Type:        schema.TypeList,
				Computed:    true,
				Elem:        &schema.Schema{Type: schema.TypeString},
				Description: "Tags associated with the device",
			},
			"labels": {
				Type:        schema.TypeMap,
				Computed:    true,
				Elem:        &schema.Schema{Type: schema.TypeString},
				Description: "Labels associated with the device",
			},
			"group": {
				Type:        schema.TypeString,
				Computed:    true,
				Description: "The group the device belongs to",
			},
		},
	}
}

func dataSourceDeviceRead(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
	c := m.(*client.Client)

	id := d.Get("id").(string)

	result, err := c.GetDevice(id)
	if err != nil {
		return diag.FromErr(err)
	}

	d.SetId(result.ID)
	d.Set("name", result.Name)
	d.Set("ip", result.IP)
	d.Set("mac", result.MAC)
	d.Set("os", result.OS)
	d.Set("arch", result.Arch)
	d.Set("status", result.Status)
	d.Set("agent_id", result.AgentID)
	d.Set("tags", result.Tags)
	d.Set("labels", result.Labels)
	d.Set("group", result.Group)

	return nil
}
