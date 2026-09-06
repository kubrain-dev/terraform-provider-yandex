# terraform-provider-yandex (Kubrain)

A Terraform provider that presents the **Yandex Cloud** resource surface
(`yandex_*`) but provisions resources on the **Kubrain** cloud. It's a drop-in
mirror: an existing YC configuration runs against Kubrain by only changing the
`source` in `required_providers`.

```hcl
terraform {
  required_providers {
    yandex = {
      source = "kubrain.dev/kubrain/yandex"
    }
  }
}
```

Arguments that have no Kubrain equivalent (e.g. `versioning`, `labels`,
`folder_id`, `platform_id`) are **accepted and silently ignored** — declared in
the schema so configs validate, but never acted on.

## Authentication

`token` (Kubrain tenant token) and `endpoint` (api-server URL) come from, in
precedence order: the `provider "yandex"` block, `$KUBRAIN_TOKEN` / `$KUBRAIN_API`,
then `~/.kubrain/config` (populated by `kubrain auth login`), then the default
`https://api.kubrain.dev`.

## Supported resources

| Resource | Kubrain backing | Notes |
|---|---|---|
| `yandex_vpc_network` | VPC | `name` maps; idempotent. |
| `yandex_vpc_subnet` | — (passthrough) | Kubrain fuses net+subnet; holds state only. |
| `yandex_kubernetes_cluster` | Cluster (control plane) | zonal master → tier `normal`, regional → `ha`; async, waits for `running`. |
| `yandex_kubernetes_node_group` | Cluster worker pool | `fixed_scale.size` → workers, `resources.memory` → RAM. One pool per cluster. |
| `yandex_storage_bucket` | Bucket | `bucket` → name, `acl` public-* → anonymous read. |
| `yandex_iam_service_account` | — (no-op) | Kubrain identity is the tenant token. |
| `yandex_iam_service_account_static_access_key` | Tenant S3 credentials | Resolves to real, working S3 keys (one pair per tenant). |
| `yandex_dns_zone` | DNS zone | `zone` → domain; exposes `name_servers`. |
| `yandex_dns_recordset` | DNS RRset | idempotent replace. |
| `yandex_container_registry` | — (no-op) | Auto-provisioned per tenant; exposes `host`. |

### Mapping caveats

- **Cluster + node group.** Kubrain couples the control plane and workers into
  one object. The cluster resource creates it (tier from the master block); the
  node group sets the worker count/RAM via scale + resize. Multiple node groups
  on one cluster collapse onto its single worker pool (last apply wins); a
  warning is emitted.
- **Immutable fields** (`network_id`, `tier`, bucket `name`/`acl`, zone `zone`)
  force replacement, matching Kubrain's "requires-replace" refusal of in-place
  changes.
- **S3 credentials** are tenant-wide, so every static access key resolves to the
  same pair.
- **Bucket delete** refuses a non-empty bucket (empty it first via S3).

## Local development

```bash
go build -o terraform-provider-yandex .
cp .terraformrc.example ~/.terraformrc   # edit the dev_overrides path
cd examples/bucket && terraform apply     # uses the local binary, no `init`
```

`go test ./...` covers the pure YC→Kubrain mapping functions.

## Not in this slice

AWS mirror (a sibling provider), registry publishing, and resource types with no
Kubrain backing (compute instances/disks, standalone load balancers, MDB,
serverless).
