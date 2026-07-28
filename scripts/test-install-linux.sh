#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TEST_ROOT="$(mktemp -d)"
trap 'rm -rf "$TEST_ROOT"' EXIT
TEST_BASE_URL="file://$TEST_ROOT"
if command -v cygpath >/dev/null 2>&1; then
  TEST_BASE_URL="file:///$(cygpath -m "$TEST_ROOT")"
fi

fail() {
  echo "FAIL: $*" >&2
  exit 1
}

create_release() {
  local release_dir="$1"
  local archive_name="$2"
  local checksum_mode="${3:-valid}"
  local payload_root="$TEST_ROOT/payload"

  rm -rf "$payload_root"
  mkdir -p "$payload_root/proxyma/dist" "$payload_root/proxyma/scripts" "$release_dir"
  printf 'server\n' >"$payload_root/proxyma/dist/Proxyma-linux-amd64"
  printf 'ctl\n' >"$payload_root/proxyma/dist/proxymactl-linux-amd64"
  printf 'helper\n' >"$payload_root/proxyma/scripts/deployment-endpoint.sh"
  printf 'uninstall\n' >"$payload_root/proxyma/scripts/uninstall-linux-release.sh"
  cat >"$payload_root/proxyma/scripts/install-linux-release.sh" <<'EOF'
#!/usr/bin/env bash
printf '%s\n' "$GOARCH" "$@" >"$PROXYMA_TEST_RESULT"
EOF
  chmod +x "$payload_root/proxyma/scripts/install-linux-release.sh"
  tar -C "$payload_root" -czf "$release_dir/$archive_name" proxyma

  if [[ "$checksum_mode" == "valid" ]]; then
    (
      cd "$release_dir"
      sha256sum "$archive_name" >SHA256SUMS
    )
  else
    printf '%064d  %s\n' 0 "$archive_name" >"$release_dir/SHA256SUMS"
  fi
}

LATEST_DIR="$TEST_ROOT/releases/latest/download"
create_release "$LATEST_DIR" "proxyma_linux_amd64.tar.gz"
RESULT_FILE="$TEST_ROOT/result"

PROXYMA_RELEASE_BASE_URL="$TEST_BASE_URL" \
PROXYMA_MACHINE="x86_64" \
PROXYMA_ALLOW_NON_LINUX_TEST=1 \
PROXYMA_TEST_RESULT="$RESULT_FILE" \
  bash "$ROOT/scripts/install-proxyma.sh" --port 8443 --no-bootstrap-admin

mapfile -t result <"$RESULT_FILE"
[[ "${result[0]}" == "amd64" ]] || fail "architecture was not forwarded"
[[ "${result[1]}" == "--port" && "${result[2]}" == "8443" ]] || fail "port arguments were not forwarded"
[[ "${result[3]}" == "--no-bootstrap-admin" ]] || fail "bootstrap argument was not forwarded"

EXACT_DIR="$TEST_ROOT/releases/download/v1.2.3"
create_release "$EXACT_DIR" "proxyma_linux_amd64.tar.gz"
PROXYMA_RELEASE_BASE_URL="$TEST_BASE_URL" \
PROXYMA_MACHINE="amd64" \
PROXYMA_ALLOW_NON_LINUX_TEST=1 \
PROXYMA_TEST_RESULT="$RESULT_FILE" \
  bash "$ROOT/scripts/install-proxyma.sh" --version v1.2.3 -- --help
grep -Fx -- "--help" "$RESULT_FILE" >/dev/null || fail "arguments after -- were not forwarded"

if PROXYMA_RELEASE_BASE_URL="$TEST_BASE_URL" PROXYMA_MACHINE=x86_64 \
  PROXYMA_ALLOW_NON_LINUX_TEST=1 bash "$ROOT/scripts/install-proxyma.sh" --version v1.2 >/dev/null 2>&1; then
  fail "invalid version was accepted"
fi

if PROXYMA_RELEASE_BASE_URL="$TEST_BASE_URL" PROXYMA_MACHINE=ppc64le \
  PROXYMA_ALLOW_NON_LINUX_TEST=1 bash "$ROOT/scripts/install-proxyma.sh" >/dev/null 2>&1; then
  fail "unsupported architecture was accepted"
fi

BAD_DIR="$TEST_ROOT/releases/download/v1.2.4"
create_release "$BAD_DIR" "proxyma_linux_amd64.tar.gz" invalid
if PROXYMA_RELEASE_BASE_URL="$TEST_BASE_URL" PROXYMA_MACHINE=x86_64 \
  PROXYMA_ALLOW_NON_LINUX_TEST=1 PROXYMA_TEST_RESULT="$RESULT_FILE" \
  bash "$ROOT/scripts/install-proxyma.sh" --version v1.2.4 >/dev/null 2>&1; then
  fail "checksum mismatch was accepted"
fi

UNSAFE_DIR="$TEST_ROOT/releases/download/v1.2.5"
mkdir -p "$UNSAFE_DIR" "$TEST_ROOT/unsafe-source"
printf 'unsafe\n' >"$TEST_ROOT/unsafe-source/payload"
tar -C "$TEST_ROOT/unsafe-source" -czf "$UNSAFE_DIR/proxyma_linux_amd64.tar.gz" \
  --transform='s#^payload#proxyma/../escape#' payload
(
  cd "$UNSAFE_DIR"
  sha256sum proxyma_linux_amd64.tar.gz >SHA256SUMS
)
if PROXYMA_RELEASE_BASE_URL="$TEST_BASE_URL" PROXYMA_MACHINE=x86_64 \
  PROXYMA_ALLOW_NON_LINUX_TEST=1 PROXYMA_TEST_RESULT="$RESULT_FILE" \
  bash "$ROOT/scripts/install-proxyma.sh" --version v1.2.5 >/dev/null 2>&1; then
  fail "archive path traversal was accepted"
fi

if bash "$ROOT/scripts/package-linux-release.sh" \
  amd64 v1.2.3-01 deadbeef "$TEST_ROOT/package-output" >/dev/null 2>&1; then
  fail "package script accepted an invalid prerelease version"
fi

echo "Precompiled Linux downloader tests passed."
