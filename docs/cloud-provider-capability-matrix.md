# Cloud Provider Capability Matrix

Generated from `internal/platform/cloudprovider/*/provider.go` and `registry.go`.

| Provider | Registered | Auth method | Discovery (resource types) | Create | Delete | SyncTags | Supported resource types | Notes |
|---|---|---|---|---|---|---|---|---|
| Aliyun | yes (registry.go) | AKSK, OIDC | VPC, Subnet, Instance, Cluster | instance | instance | instance (TagResources) | vpc, subnet, instance, cluster | Native HTTP (HMAC-SHA1); SDK blank-imported for future use; ListRegions via ECS DescribeRegions with hardcoded fallback |
| AWS | yes (registry.go) | AKSK, OIDC | VPC, Subnet, Instance, Cluster | instance | instance | instance (CreateTags) | vpc, subnet, instance, cluster | Native HTTP (SigV4); ListRegions returns built-in region list |
| Azure | yes (registry.go) | AKSK, OIDC, Managed Identity | VPC, Subnet, Instance, Cluster | instance (vm) | instance | instance | vpc, subnet, instance, cluster | Native HTTP (Azure RM); auth via Azure AD OAuth2 client credentials; CreateResource accepts `vm` alias |
| Tencent | yes (registry.go) | AKSK, OIDC | VPC, Subnet, Instance, Cluster | instance | instance | instance (TagResources) | vpc, subnet, instance, cluster | Native HTTP (TC3-HMAC-SHA256); ListRegions returns built-in region list |
| Huawei | yes (registry.go) | AKSK, OIDC | VPC, Subnet, Instance, Cluster | instance | instance | instance | vpc, subnet, instance, cluster | Native HTTP (HMAC-SHA256); CreateResource accepts `instance`/`ecs`/`server` aliases |
| Volcengine | yes (registry.go) | AKSK, OIDC | VPC, Subnet, Instance, Cluster | instance | instance (TerminateInstances) | instance (TagResources) | vpc, subnet, instance, cluster | Native HTTP (HMAC-SHA256); ListRegions returns built-in region list |

## Summary

- All six providers are registered in `registry.go` via `RegisterBuiltIns`.
- Common discovery surface: `DiscoverVPCs`, `DiscoverSubnets`, `DiscoverInstances`, `DiscoverClusters` → resource types `vpc`, `subnet`, `instance`, `cluster`.
- Mutating operations (`CreateResource`/`DeleteResource`/`SyncTags`) are implemented for `instance` only across all providers; other resource types return an "unsupported resource type" error.
- Auth: AKSK + OIDC for all; Azure additionally supports Managed Identity.
- None depends on official cloud SDKs (native HTTP with HMAC signing); Aliyun keeps a blank SDK import for future migration.
