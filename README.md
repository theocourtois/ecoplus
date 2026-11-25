# hetzner-ecoplus

A Terraform provider and a small controller that cut Hetzner Cloud costs for short-lived servers. Hetzner bills per started hour: ecoplus wipes released servers and keeps them in a pool until just before their next billed hour, so a new request with a compatible spec reuses a server that is already paid for.

## Installation

The provider and the controller are released on GitHub only, not on the Terraform Registry. Download the provider archive from the latest release and install it as a [development override](https://developer.hashicorp.com/terraform/cli/config/config-file#development-overrides-for-provider-developers), or build it with `make build`. The controller image is `ghcr.io/theocourtois/hetzner-ecoplus-server`.

## Usage

```hcl
provider "hetzner-ecoplus" {
  endpoint = "https://ecoplus.internal:8080"
  token    = var.ecoplus_token
}

resource "hetzner-ecoplus_server" "sandbox" {
  name        = "sandbox-1"
  server_type = "cx22"
  location    = "nbg1"
  image       = "ubuntu-24.04"
}
```

Firewalls, networks, volumes and rDNS keep using the `hcloud` provider, attached by server ID.

## Development

```
make check
make build
```
