terraform {
  required_providers {
    yandex = {
      source = "kubrain.dev/kubrain/yandex"
    }
  }
}

# token/endpoint fall back to $KUBRAIN_TOKEN / $KUBRAIN_API and ~/.kubrain/config,
# so this block can be empty if you've run `kubrain auth login`.
provider "yandex" {
  # endpoint = "https://api.kubrain.dev"
  # token    = "<tenant token>"
}

# A service account is a no-op on Kubrain (identity is the tenant token), but is
# declared so this mirrors a real Yandex Cloud config unchanged.
resource "yandex_iam_service_account" "sa" {
  name = "devopstrain-sa"
}

# The static access key resolves to the tenant's real Kubrain S3 credentials.
resource "yandex_iam_service_account_static_access_key" "sa-static-key" {
  service_account_id = yandex_iam_service_account.sa.id
  description        = "static access key for object storage"
}

# The headline example: this creates a real bucket on Kubrain object storage.
resource "yandex_storage_bucket" "bucket" {
  access_key = yandex_iam_service_account_static_access_key.sa-static-key.access_key
  secret_key = yandex_iam_service_account_static_access_key.sa-static-key.secret_key

  bucket = "devopstrain-learning-bucket" # must be unique
  acl    = "private"

  # Accepted for compatibility and silently ignored (Kubrain has no equivalent):
  versioning {
    enabled = true
  }
}

output "bucket_domain_name" {
  value = yandex_storage_bucket.bucket.bucket_domain_name
}
