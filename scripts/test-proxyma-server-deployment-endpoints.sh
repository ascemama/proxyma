#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
source "$ROOT/scripts/deployment-endpoint.sh"

fail() {
  echo "FAIL: $1" >&2
  exit 1
}

expect_failure() {
  if "$@" >/dev/null 2>&1; then
    fail "expected command to fail: $*"
  fi
}

validate_bind_address "0.0.0.0"
validate_bind_address "proxyma.internal"
validate_bind_address "[2001:db8::10]"
expect_failure validate_bind_address ""
expect_failure validate_bind_address "2001:db8::10"
expect_failure validate_bind_address "bad/address"

validate_port "Port" "1"
validate_port "Port" "8080"
validate_port "Port" "65535"
expect_failure validate_port "Port" "0"
expect_failure validate_port "Port" "65536"
expect_failure validate_port "Port" "http"
expect_failure validate_port "Port" "999999999999999999999999"

parse_listen_address "[2001:db8::10]:8443"
[[ "$PARSED_BIND_ADDRESS" == "[2001:db8::10]" && "$PARSED_PORT" == "8443" ]] || fail "IPv6 listener parsing"
parse_listen_address ":9090"
[[ "$PARSED_BIND_ADDRESS" == "0.0.0.0" && "$PARSED_PORT" == "9090" ]] || fail "wildcard listener parsing"

TEST_DIR="$(mktemp -d)"
trap 'rm -rf "$TEST_DIR"' EXIT
CONFIG_FILE="$TEST_DIR/proxyma.config.json"
cat > "$CONFIG_FILE" <<'EOF'
{
  "server": {"listen_address": "127.0.0.1:8080"},
  "unrelated": {"preserve": true}
}
EOF

[[ "$(read_json_listen_address "$CONFIG_FILE")" == "127.0.0.1:8080" ]] || fail "listener read"
update_json_listen_address "$CONFIG_FILE" "[::]:9443"
[[ "$(read_json_listen_address "$CONFIG_FILE")" == "[::]:9443" ]] || fail "listener update"
grep -q '"unrelated": {"preserve": true}' "$CONFIG_FILE" || fail "unrelated JSON preservation"

LINUX_HELP="$(bash "$ROOT/scripts/install-proxyma-server-linux-release.sh" --help)"
DOCKER_HELP="$(bash "$ROOT/scripts/install-proxyma-server-docker.sh" --help)"
grep -q -- '--bind-address ADDRESS' <<<"$LINUX_HELP" || fail "Linux bind-address help"
grep -q -- '--port PORT' <<<"$LINUX_HELP" || fail "Linux port help"
grep -q -- '--bind-address ADDRESS' <<<"$DOCKER_HELP" || fail "Docker bind-address help"
grep -q -- '--host-port PORT' <<<"$DOCKER_HELP" || fail "Docker host-port help"
for option in --http --https --self-signed --tls-cert --tls-key --tls-server-name; do
  grep -q -- "$option" <<<"$LINUX_HELP" || fail "Linux $option help"
  grep -q -- "$option" <<<"$DOCKER_HELP" || fail "Docker $option help"
done

expect_failure bash "$ROOT/scripts/install-proxyma-server-linux-release.sh" --port 0
expect_failure bash "$ROOT/scripts/install-proxyma-server-linux-release.sh" --port 65536
expect_failure bash "$ROOT/scripts/install-proxyma-server-linux-release.sh" --http --self-signed
expect_failure bash "$ROOT/scripts/install-proxyma-server-linux-release.sh" --tls-cert cert.pem
expect_failure bash "$ROOT/scripts/install-proxyma-server-linux-release.sh" --https --tls-server-name proxyma.example.com
expect_failure bash "$ROOT/scripts/install-proxyma-server-linux-release.sh" --tls-server-name "bad/name"
expect_failure bash "$ROOT/scripts/install-proxyma-server-docker.sh" --host-port invalid
expect_failure bash "$ROOT/scripts/install-proxyma-server-docker.sh" --bind-address "bad/address"
expect_failure bash "$ROOT/scripts/install-proxyma-server-docker.sh" --http --https
expect_failure bash "$ROOT/scripts/install-proxyma-server-docker.sh" --tls-key key.pem
expect_failure bash "$ROOT/scripts/install-proxyma-server-docker.sh" --tls-server-name "bad/name"

grep -q 'BIND_ADDRESS="0.0.0.0"' "$ROOT/scripts/install-proxyma-server-linux-release.sh" || fail "Linux default address"
grep -q 'LISTEN_PORT="8080"' "$ROOT/scripts/install-proxyma-server-linux-release.sh" || fail "Linux default port"
grep -q 'PROXYMA_BIND_ADDRESS:-0.0.0.0' "$ROOT/docker-compose.example.yml" || fail "Compose default address"
grep -q 'PROXYMA_HOST_PORT:-8080' "$ROOT/docker-compose.example.yml" || fail "Compose default host port"
grep -q 'PROXYMA_LISTEN=0.0.0.0:8080' "$ROOT/docker-compose.example.yml" || fail "fixed container listener"
grep -q '"scheme": "$CONFIG_SCHEME"' "$ROOT/scripts/install-proxyma-server-linux-release.sh" || fail "Linux fresh transport configuration"
grep -q '"scheme": "$CONFIG_SCHEME"' "$ROOT/scripts/install-proxyma-server-docker.sh" || fail "Docker fresh transport configuration"
grep -q 'LEGACY_SCHEME="http"' "$ROOT/scripts/install-proxyma-server-linux-release.sh" || fail "Linux legacy HTTP preservation"
grep -q 'LEGACY_SCHEME="http"' "$ROOT/scripts/install-proxyma-server-docker.sh" || fail "Docker legacy HTTP preservation"
grep -q 'https://127.0.0.1:8080/' "$ROOT/docker-compose.example.yml" || fail "HTTPS health check"
grep -q 'http://127.0.0.1:8080/' "$ROOT/docker-compose.example.yml" || fail "HTTP health-check fallback"
if grep -q 'ALLOW_INSECURE_USER_COOKIES' "$ROOT/docker-compose.example.yml"; then
  fail "obsolete insecure-cookie environment setting"
fi

echo "Deployment endpoint and transport tests passed."
