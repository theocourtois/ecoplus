resource "hetzner-ecoplus_server" "sandbox" {
  name        = "sandbox-1"
  server_type = "cx22"
  location    = "nbg1"
  image       = "ubuntu-24.04"
  ssh_keys    = ["deploy"]

  labels = {
    team = "platform"
  }
}

resource "hcloud_firewall_attachment" "sandbox" {
  firewall_id = hcloud_firewall.default.id
  server_ids  = [tonumber(hetzner-ecoplus_server.sandbox.id)]
}
