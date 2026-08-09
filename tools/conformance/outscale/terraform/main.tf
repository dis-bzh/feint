# Conformance fixture: the real Outscale Terraform provider, pointed at the emulator.
#
# The endpoint carries the whole API path. Their documentation gives the shape —
# `endpoint = "https://api.eu-west-2.outscale.com/api/v1"` — so the version
# segment belongs to the value rather than being appended by the provider. The
# top-level `region`, `endpoints` and `insecure` arguments are deprecated in
# favour of this `api` block; using them costs a warning on every command.
#
# Getting that wrong is not a warning, it is a six-minute wait: with the version
# segment missing, the provider retries with backoff until it times out, and the
# failure reads like a slow emulator rather than a misdirected client.

terraform {
  required_version = ">= 1.7.0"

  required_providers {
    outscale = {
      source  = "outscale/outscale"
      version = "~> 1.7"
    }
  }
}

variable "endpoint" {
  type        = string
  description = "Base URL of the running feint emulator, without the API path."
  default     = "http://127.0.0.1:4599"
}

provider "outscale" {
  access_key_id = "AAAAAAAAAAAAAAAAAAAA"
  secret_key_id = "BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB"

  api {
    endpoint = "${var.endpoint}/api/v1"
    region   = "eu-west-2"
  }
}

# Tags are here because the provider calls CreateTags on almost every resource,
# and because their order is a permanent diff waiting to happen: the emulator
# sorts them, so two reads of an unchanged Net are identical.
resource "outscale_net" "conformance" {
  ip_range = "10.70.0.0/16"

  tags {
    key   = "name"
    value = "feint-conformance"
  }
}

resource "outscale_subnet" "conformance" {
  net_id   = outscale_net.conformance.net_id
  ip_range = "10.70.1.0/24"
}

# A keypair, because a machine nobody can log into proves nothing — and because
# the provider addresses it by KeypairId on destroy while creating it by name.
# The emulator publishes both, and they are the same identity.
resource "outscale_keypair" "conformance" {
  keypair_name = "feint-conformance"
  public_key   = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIIr6pEFlAFO3YU0DNW/r8SkpjdbptN9ockkO2BtIolSD conformance@feint"
}

# A volume, because the provider creates one, reads it back and links it — and
# because nothing else in this repository drives CreateVolume. The oapi-cli suite
# never touches one, which is exactly why the routes were missing.
resource "outscale_volume" "conformance" {
  subregion_name = "eu-west-2a"
  size           = 10
}

# The catalogue read back through the provider's own data source, because a
# response the provider accepts is not the same as one it can decode. This block
# is here for the reason the volume above is: its absence hid a crash. ReadImages
# answered without BlockDeviceMappings, the provider dereferenced the nil, and
# the plugin died with "Plugin did not respond" — a message naming neither the
# field nor the call, and one no unit test reading JSON can produce.
data "outscale_images" "conformance" {
  filter {
    name   = "image_ids"
    values = ["ami-00000001"]
  }
}

resource "outscale_vm" "conformance" {
  image_id     = "ami-12345678"
  vm_type      = "tinav6.c1r1p2"
  subnet_id    = outscale_subnet.conformance.subnet_id
  keypair_name = outscale_keypair.conformance.keypair_name

  tags {
    key   = "name"
    value = "feint-conformance"
  }
}

output "vm_id" {
  value = outscale_vm.conformance.vm_id
}

output "net_id" {
  value = outscale_net.conformance.net_id
}

output "volume_id" {
  value = outscale_volume.conformance.volume_id
}

# The link, which is where the volume meets the machine — and the only thing
# that drives ReadVolumes with a LinkVolumeVmIds filter. The provider does not
# poll the volume, it polls the link: ReadVolumes with VolumeIds +
# LinkVolumeVmIds until LinkedVolumes[0].State reads "attached". While that
# filter was refused, the wait failed outright and the resource could not be
# used at all — with a volume in this file the whole time, unlinked.
resource "outscale_volume_link" "conformance" {
  device_name = "/dev/sdb"
  volume_id   = outscale_volume.conformance.volume_id
  vm_id       = outscale_vm.conformance.vm_id
}

output "keypair_id" {
  value = outscale_keypair.conformance.keypair_id
}

output "volume_link_state" {
  value = outscale_volume_link.conformance.state
}

# Read from the data source rather than from the catalogue constant: an output
# that never touches it would let the block be dropped from the graph.
output "image_id" {
  value = data.outscale_images.conformance.images[0].image_id
}
