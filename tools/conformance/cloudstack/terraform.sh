#!/usr/bin/env bash
# Conformance check: the published CloudStack Terraform provider against the emulator.
#
# Drives cloudstack/cloudstack 0.7.0 from the registry, not a build of its
# repository: the claim is about what people install. One instance is applied, the
# second plan must be empty (a plan that is not is the provider reading back
# something the pack does not hold steady), and the destroy must expunge it.
#
# The pack is experimental, so the emulator must have been started with
# FEINT_EXPERIMENTAL_PACKS=cloudstack. To check the pack against the official
# simulator instead, point this at it and name its catalogue:
#
#   CLOUDSTACK_KEY=... CLOUDSTACK_SECRET=... TF_VAR_zone=Sandbox-simulator \
#   TF_VAR_offering='Small Instance' TF_VAR_template='CentOS 5.6 (64-bit) no GUI (Simulator)' \
#     tools/conformance/cloudstack/terraform.sh http://127.0.0.1:8080
#
# The binary is OpenTofu when it is installed, Terraform otherwise; FEINT_TF forces
# either.
#
# Usage: tools/conformance/cloudstack/terraform.sh [endpoint]   (default http://127.0.0.1:4599)
set -euo pipefail

ENDPOINT="${1:-http://127.0.0.1:4599}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# Never let a client reach anything but a local server. api_url is a required
# argument of the provider, so a missing endpoint fails rather than falling back,
# and this is the check that it is the one we meant.
# shellcheck source=/dev/null
. "$SCRIPT_DIR/../guard.sh"
guard_local "$ENDPOINT"

TF="${FEINT_TF:-}"
if [ -z "$TF" ]; then
  if command -v tofu >/dev/null 2>&1; then TF=tofu; else TF=terraform; fi
fi
command -v "$TF" >/dev/null 2>&1 || { echo "FAIL: neither tofu nor terraform is installed" >&2; exit 1; }

WORK="$(mktemp -d)"
cd "$WORK"

# The configuration lives here and not in a main.tf beside this script, on purpose.
# A .tf file under tools/conformance is read by internal/cli as a stack CI applies
# and published as proof of the provider version it pins, and no workflow runs this
# suite yet. When one does, this moves to terraform/main.tf and the page claims it.
cat >main.tf <<'HCL'
# One instance, applied, planned again and destroyed: the smallest stack that
# exercises the provider's whole path through the pack (zone, template and offering
# lookups by name, deploy, the job poll, read-back, destroy with expunge).
#
# The names default to the emulator's catalogue and are overridden through
# TF_VAR_* to point the same configuration at the official simulator.
#
terraform {
  required_providers {
    cloudstack = { source = "cloudstack/cloudstack", version = "0.7.0" }
  }
}

variable "api_url"    { type = string }
variable "api_key"    { type = string }
variable "secret_key" { type = string }
variable "zone"       { type = string }
variable "offering"   { type = string }
variable "template"   { type = string }

provider "cloudstack" {
  api_url    = var.api_url
  api_key    = var.api_key
  secret_key = var.secret_key
}

resource "cloudstack_instance" "vm" {
  name             = "tf-vm"
  service_offering = var.offering
  template         = var.template
  zone             = var.zone
  expunge          = true
}

output "ip"    { value = cloudstack_instance.vm.ip_address }
HCL

export TF_IN_AUTOMATION=1 CHECKPOINT_DISABLE=1
export TF_VAR_api_url="${ENDPOINT}/client/api"
export TF_VAR_api_key="${CLOUDSTACK_KEY:-feint-api-key}"
export TF_VAR_secret_key="${CLOUDSTACK_SECRET:-feint-secret-key}"
export TF_VAR_zone="${TF_VAR_zone:-feint-zone}"
export TF_VAR_offering="${TF_VAR_offering:-feint.small}"
export TF_VAR_template="${TF_VAR_template:-feint-debian-12}"

fail() { echo "FAIL: $*" >&2; exit 1; }
ok() { echo "  ok: $*"; }

# An apply that fails leaves a machine in the emulator's store, and the next run
# would find it. The destroy happens on the error path too.
APPLIED=0
cleanup() {
  local status=$?
  if [ "$APPLIED" = 1 ]; then "$TF" destroy -auto-approve -no-color >/dev/null 2>&1 || true; fi
  rm -rf "$WORK"
  exit "$status"
}
trap cleanup EXIT

"$TF" init -no-color >/dev/null || fail "init: the provider could not be installed"
ok "init (cloudstack/cloudstack 0.7.0 from the registry)"

APPLIED=1
"$TF" apply -auto-approve -no-color >apply.log 2>&1 || { cat apply.log >&2; fail "apply"; }
grep -q "Resources: 1 added" apply.log || fail "apply did not create the instance"
ok "apply: instance created"

"$TF" output -raw ip | grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$' || fail "the instance has no address"
ok "the instance reads back an address"

# -detailed-exitcode: 0 is an empty plan, 2 is changes, 1 is an error.
rc=0
"$TF" plan -detailed-exitcode -no-color >plan.log 2>&1 || rc=$?
[ "$rc" = 0 ] || { cat plan.log >&2; fail "the second plan is not empty (exit $rc)"; }
ok "second plan is empty"

"$TF" destroy -auto-approve -no-color >destroy.log 2>&1 || { cat destroy.log >&2; fail "destroy"; }
APPLIED=0
grep -q "Resources: 1 destroyed" destroy.log || fail "destroy did not remove the instance"
ok "destroy: instance expunged"

echo "terraform: all checks passed"
