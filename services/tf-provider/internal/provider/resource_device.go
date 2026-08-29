package provider

import (
	"context"
	"time"

	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"

	"github.com/Levango7/OpsMesh/services/tf-provider/internal/client"
)

func resourceDevice() *schema.Resource {
	return &schema.Resource{
		CreateContext: resourceDeviceCreate,
		ReadContext:   resourceDeviceRead,
		UpdateContext: resourceDeviceUpdate,
		DeleteContext: resourceDeviceDelete,
		Schema: map[string]*schema.Schema{
			"name": {
				Type:        schema.TypeString,
				Required:    true,
				Description: "The name of the device",
			},
			"ip": {
				Type:        schema.TypeString,
				Optional:    true,
				Description: "The IP address of the device",
			},
			"mac": {
				Type:        schema.TypeString,
				Optional:    true,
				Description: "The MAC address of the device",
			},
			"os": {
				Type:        schema.TypeString,
				Optional:    true,
				Description: "The operating system of the device",
			},
			"arch": {
				Type:        schema.TypeString,
				Optional:    true,
				Description: "The architecture of the device",
			},
			"status": {
				Type:        schema.TypeString,
				Computed:    true,
				Description: "The current status of the device",
			},
			"agent_id": {
				Type:        schema.TypeString,
				Optional:    true,
				Description: "The agent ID associated with the device",
			},
			"tags": {
				Type:        schema.TypeList,
				Optional:    true,
				Elem:        &schema.Schema{Type: schema.TypeString},
				Description: "Tags associated with the device",
			},
			"labels": {
				Type:        schema.TypeMap,
				Optional:    true,
				Elem:        &schema.Schema{Type: schema.TypeString},
				Description: "Labels associated with the device",
			},
			"group": {
				Type:        schema.TypeString,
				Optional:    true,
				Description: "The group the device belongs to",
			},
		},
	}
}

func resourceDeviceCreate(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
	c := m.(*client.Client)

	device := &client.Device{
		Name:    d.Get("name").(string),
		IP:      d.Get("ip").(string),
		MAC:     d.Get("mac").(string),
		OS:      d.Get("os").(string),
		Arch:    d.Get("arch").(string),
		AgentID: d.Get("agent_id").(string),
		Group:   d.Get("group").(string),
	}

	if v, ok := d.GetOk("tags"); ok {
		for _, t := range v.([]interface{}) {
			device.Tags = append(device.Tags, t.(string))
		}
	}

	if v, ok := d.GetOk("labels"); ok {
		device.Labels = make(map[string]string)
		for k, val := range v.(map[string]interface{}) {
			device.Labels[k] = val.(string)
		}
	}

	result, err := c.CreateDevice(device)
	if err != nil {
		return diag.FromErr(err)
	}

	d.SetId(result.ID)

	return resourceDeviceRead(ctx, d, m)
}

func resourceDeviceRead(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
	c := m.(*client.Client)

	result, err := c.GetDevice(d.Id())
	if err != nil {
		d.SetId("")
		return diag.FromErr(err)
	}

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

func resourceDeviceUpdate(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
	c := m.(*client.Client)

	device := &client.Device{
		ID:      d.Id(),
		Name:    d.Get("name").(string),
		IP:      d.Get("ip").(string),
		MAC:     d.Get("mac").(string),
		OS:      d.Get("os").(string),
		Arch:    d.Get("arch").(string),
		AgentID: d.Get("agent_id").(string),
		Group:   d.Get("group").(string),
	}

	if v, ok := d.GetOk("tags"); ok {
		for _, t := range v.([]interface{}) {
			device.Tags = append(device.Tags, t.(string))
		}
	}

	if v, ok := d.GetOk("labels"); ok {
		device.Labels = make(map[string]string)
		for k, val := range v.(map[string]interface{}) {
			device.Labels[k] = val.(string)
		}
	}

	_, err := c.UpdateDevice(device)
	if err != nil {
		return diag.FromErr(err)
	}

	d.Set("last_updated", time.Now().Format(time.RFC3339))

	return resourceDeviceRead(ctx, d, m)
}

func resourceDeviceDelete(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
	c := m.(*client.Client)

	err := c.DeleteDevice(d.Id())
	if err != nil {
		return diag.FromErr(err)
	}

	d.SetId("")
	return nil
}
