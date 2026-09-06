terraform {
  required_providers {
    yandex = {
      source = "kubrain.dev/kubrain/yandex"
    }
  }
}

provider "yandex" {
  # endpoint / token fall back to env and ~/.kubrain/config.
}

resource "yandex_vpc_network" "net" {
  name = "tf-demo-net"
}

# Passthrough — Kubrain fuses network + subnet, so this only wires the graph.
resource "yandex_vpc_subnet" "subnet" {
  name           = "tf-demo-subnet"
  network_id     = yandex_vpc_network.net.id
  zone           = "ru-central1-a"
  v4_cidr_blocks = ["10.10.0.0/24"]
}

# A zonal master maps to Kubrain tier `normal` (one dedicated control plane).
# A `regional { }` block instead would map to tier `ha` (3 control planes).
resource "yandex_kubernetes_cluster" "cluster" {
  name       = "tf-demo"
  network_id = yandex_vpc_network.net.id

  master {
    version = "v1.32.4"
    zonal {
      zone      = "ru-central1-a"
      subnet_id = yandex_vpc_subnet.subnet.id
    }
  }
}

# The node group sets the worker pool shape: size -> worker count,
# resources.memory (GiB) -> per-worker RAM. Kubrain has one worker pool.
resource "yandex_kubernetes_node_group" "workers" {
  name       = "tf-demo-workers"
  cluster_id = yandex_kubernetes_cluster.cluster.id

  scale_policy {
    fixed_scale {
      size = 2
    }
  }

  instance_template {
    platform_id = "standard-v3" # ignored
    resources {
      memory = 8
      cores  = 2 # ignored (Kubrain derives vCPU from RAM)
    }
  }
}

output "endpoint" {
  value = yandex_kubernetes_cluster.cluster.master.external_v4_endpoint
}
