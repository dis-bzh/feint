#!/usr/bin/env bash
# Conformance check: the ngine_io.cloudstack Ansible collection against the emulator.
#
# Runs the module family behind cs_instance (zone_info, instance, instance_info)
# through instance.yml. The collection is the published one (3.3.0), installed into
# a throwaway directory; FEINT_CLOUDSTACK_COLLECTION may name a checkout of
# ansible-collection-cloudstack to test instead, laid out as
# <dir>/ansible_collections/ngine_io/cloudstack.
#
# The pack is experimental, so the emulator must have been started with
# FEINT_EXPERIMENTAL_PACKS=cloudstack. To check the pack against the official
# simulator, point this at it, give its keys and name its catalogue:
#
#   CLOUDSTACK_KEY=... CLOUDSTACK_SECRET=... FEINT_CS_ZONE=Sandbox-simulator \
#   FEINT_CS_OFFERING='Small Instance' FEINT_CS_TEMPLATE='CentOS 5.6 (64-bit) no GUI (Simulator)' \
#     tools/conformance/cloudstack/ansible.sh http://127.0.0.1:8080
#
# Usage: tools/conformance/cloudstack/ansible.sh [endpoint]   (default http://127.0.0.1:4599)
set -euo pipefail

ENDPOINT="${1:-http://127.0.0.1:4599}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CS_VERSION="5.1.0"
COLLECTION_VERSION="3.3.0"

# shellcheck source=/dev/null
. "$SCRIPT_DIR/../guard.sh"
guard_local "$ENDPOINT"

command -v python3 >/dev/null 2>&1 || { echo "FAIL: python3 is not installed" >&2; exit 1; }

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

python3 -m venv "$WORK/venv"
"$WORK/venv/bin/pip" install --quiet ansible-core "cs==${CS_VERSION}"

if [ -n "${FEINT_CLOUDSTACK_COLLECTION:-}" ]; then
  export ANSIBLE_COLLECTIONS_PATH="$FEINT_CLOUDSTACK_COLLECTION"
else
  "$WORK/venv/bin/ansible-galaxy" collection install "ngine_io.cloudstack:${COLLECTION_VERSION}" -p "$WORK/collections" >/dev/null
  export ANSIBLE_COLLECTIONS_PATH="$WORK/collections"
fi

# An empty home keeps the library from finding a real ~/.cloudstack.ini.
export HOME="$WORK"
export CLOUDSTACK_ENDPOINT="${ENDPOINT}/client/api"
export CLOUDSTACK_KEY="${CLOUDSTACK_KEY:-feint-api-key}"
export CLOUDSTACK_SECRET="${CLOUDSTACK_SECRET:-feint-secret-key}"
export ANSIBLE_NOCOLOR=1 ANSIBLE_LOCALHOST_WARNING=False ANSIBLE_INVENTORY_UNPARSED_WARNING=False
export ANSIBLE_PYTHON_INTERPRETER="$WORK/venv/bin/python"

VARS="$(printf '{"zone_name": "%s", "offering_name": "%s", "template_name": "%s"}' \
  "${FEINT_CS_ZONE:-feint-zone}" "${FEINT_CS_OFFERING:-feint.small}" "${FEINT_CS_TEMPLATE:-feint-debian-12}")"

"$WORK/venv/bin/ansible-playbook" -i localhost, -c local -e "$VARS" "$SCRIPT_DIR/instance.yml" 2>&1 | tee "$WORK/play.log"
grep -Eq 'failed=0' "$WORK/play.log" || { echo "FAIL: the playbook reported a failure" >&2; exit 1; }
echo "ansible: all checks passed"
