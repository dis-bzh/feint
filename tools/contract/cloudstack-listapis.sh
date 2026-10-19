#!/usr/bin/env bash
# Fetch the raw answer of `listApis` from a running CloudStack management server.
#
# CloudStack publishes no OpenAPI document; the server describes itself, and this
# is the source tools/contract/extract-cloudstack.py reads (see its header). The
# answer is 3.6 MB for the 865 commands of 4.23.0.0, so it is fetched on demand
# into .upstream/ (which is not versioned) rather than committed.
#
#   docker run -d --network host apache/cloudstack-simulator
#   tools/contract/cloudstack-listapis.sh > .upstream/cloudstack-listapis.json
#
# The simulator's own account is admin/password in the root domain. For any other
# server, CLOUDSTACK_USER, CLOUDSTACK_PASSWORD and CLOUDSTACK_DOMAIN name yours.
# The account sees the commands its role allows, and the artefact is the surface of
# the account that fetched it, so use an admin: a user's answer is shorter.
#
# Usage: tools/contract/cloudstack-listapis.sh [endpoint]   (default http://127.0.0.1:8080)
set -euo pipefail

ENDPOINT="${1:-http://127.0.0.1:8080}"
API="${ENDPOINT}/client/api"
JAR="$(mktemp)"
trap 'rm -f "$JAR"' EXIT

command -v curl >/dev/null 2>&1 || { echo "FAIL: curl is not installed" >&2; exit 1; }
command -v python3 >/dev/null 2>&1 || { echo "FAIL: python3 is not installed" >&2; exit 1; }

login="$(curl -fsS -c "$JAR" \
  --data-urlencode "command=login" \
  --data-urlencode "username=${CLOUDSTACK_USER:-admin}" \
  --data-urlencode "password=${CLOUDSTACK_PASSWORD:-password}" \
  --data-urlencode "domain=${CLOUDSTACK_DOMAIN:-/}" \
  --data-urlencode "response=json" "$API")"
session="$(printf '%s' "$login" | python3 -c 'import sys, json; print(json.load(sys.stdin)["loginresponse"]["sessionkey"])')" \
  || { echo "FAIL: the server refused the login: $login" >&2; exit 1; }

curl -fsS -b "$JAR" \
  --data-urlencode "command=listApis" \
  --data-urlencode "sessionkey=${session}" \
  --data-urlencode "response=json" "$API"
