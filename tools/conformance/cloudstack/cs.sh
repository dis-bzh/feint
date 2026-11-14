#!/usr/bin/env bash
# Conformance check: drive the emulator with the Python `cs` library.
#
# `cs` is the client under ngine_io.cloudstack, the Ansible collection, so a run
# of this is the closest thing to running its modules without Ansible: the same
# signing, the same GET, the same polling of queryAsyncJobResult (fetch_result).
#
# The pack is experimental, so the emulator must have been started with
# FEINT_EXPERIMENTAL_PACKS=cloudstack. This script does not set it: it drives a
# server that is already running, like every other suite.
#
# Usage: tools/conformance/cloudstack/cs.sh [endpoint]   (default http://127.0.0.1:4599)
set -euo pipefail

ENDPOINT="${1:-http://127.0.0.1:4599}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CS_VERSION="5.1.0"

# shellcheck source=/dev/null
. "$SCRIPT_DIR/../guard.sh"
guard_local "$ENDPOINT"

command -v python3 >/dev/null 2>&1 || { echo "FAIL: python3 is not installed" >&2; exit 1; }

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

python3 -m venv "$WORK/venv"
"$WORK/venv/bin/pip" install --quiet "cs==${CS_VERSION}"

# The credentials are the pack's own public ones; reading them from `feint env`
# would make a missing pack print nothing and the client fall back to a profile.
# They can be overridden to point the same script at the official simulator, which
# is how the pack is checked against it (oracle.py says how to start one).
export CLOUDSTACK_ENDPOINT="${ENDPOINT}/client/api"
export CLOUDSTACK_KEY="${CLOUDSTACK_KEY:-feint-api-key}"
export CLOUDSTACK_SECRET="${CLOUDSTACK_SECRET:-feint-secret-key}"
# The library also reads ~/.cloudstack.ini; an empty directory keeps it from
# finding a real profile.
export HOME="$WORK"

"$WORK/venv/bin/python" "$SCRIPT_DIR/cs_drive.py"
