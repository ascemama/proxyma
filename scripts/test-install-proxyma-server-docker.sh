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
  local version="$2"
  local checksum_mode="${3:-valid}"
  local payload_root="$TEST_ROOT/payload"

  rm -rf "$payload_root"
  mkdir -p "$payload_root/proxyma/scripts" "$release_dir"
  printf 'services: {}\n' >"$payload_root/proxyma/docker-compose.example.yml"
  printf 'helper\n' >"$payload_root/proxyma/scripts/deployment-endpoint.sh"
  printf 'uninstall\n' >"$payload_root/proxyma/scripts/uninstall-proxyma-server-docker.sh"
  cat >"$payload_root/proxyma/scripts/install-proxyma-server-docker.sh" <<'EOF'
#!/usr/bin/env bash
printf '%s\n' "$@" >"$PROXYMA_TEST_RESULT"
EOF
  chmod +x "$payload_root/proxyma/scripts/install-proxyma-server-docker.sh"
  cat >"$payload_root/proxyma/RELEASE" <<EOF
name=Proxyma
version=$version
commit=deadbeef
deployment=docker
EOF
  tar -C "$payload_root" -czf "$release_dir/proxyma_docker.tar.gz" proxyma

  if [[ "$checksum_mode" == "valid" ]]; then
    (
      cd "$release_dir"
      sha256sum proxyma_docker.tar.gz >SHA256SUMS
    )
  else
    printf '%064d  proxyma_docker.tar.gz\n' 0 >"$release_dir/SHA256SUMS"
  fi
}

LATEST_DIR="$TEST_ROOT/releases/latest/download"
create_release "$LATEST_DIR" v1.2.2
RESULT_FILE="$TEST_ROOT/result"

PROXYMA_RELEASE_BASE_URL="$TEST_BASE_URL" \
PROXYMA_ALLOW_NON_LINUX_TEST=1 \
PROXYMA_TEST_RESULT="$RESULT_FILE" \
  bash "$ROOT/scripts/download-proxyma-server-docker.sh" \
    --host-port 8443 --no-bootstrap-admin

mapfile -t result <"$RESULT_FILE"
[[ "${result[0]}" == "--host-port" && "${result[1]}" == "8443" ]] || fail "port arguments were not forwarded"
[[ "${result[2]}" == "--no-bootstrap-admin" ]] || fail "bootstrap argument was not forwarded"

EXACT_DIR="$TEST_ROOT/releases/download/v1.2.3"
create_release "$EXACT_DIR" v1.2.3
PROXYMA_RELEASE_BASE_URL="$TEST_BASE_URL" \
PROXYMA_ALLOW_NON_LINUX_TEST=1 \
PROXYMA_TEST_RESULT="$RESULT_FILE" \
  bash "$ROOT/scripts/download-proxyma-server-docker.sh" --version v1.2.3 -- --help
grep -Fx -- "--help" "$RESULT_FILE" >/dev/null || fail "arguments after -- were not forwarded"

if PROXYMA_RELEASE_BASE_URL="$TEST_BASE_URL" PROXYMA_ALLOW_NON_LINUX_TEST=1 \
  bash "$ROOT/scripts/download-proxyma-server-docker.sh" --version v1.2 >/dev/null 2>&1; then
  fail "invalid version was accepted"
fi

BAD_DIR="$TEST_ROOT/releases/download/v1.2.4"
create_release "$BAD_DIR" v1.2.4 invalid
if PROXYMA_RELEASE_BASE_URL="$TEST_BASE_URL" PROXYMA_ALLOW_NON_LINUX_TEST=1 \
  PROXYMA_TEST_RESULT="$RESULT_FILE" \
  bash "$ROOT/scripts/download-proxyma-server-docker.sh" --version v1.2.4 >/dev/null 2>&1; then
  fail "checksum mismatch was accepted"
fi

MISMATCH_DIR="$TEST_ROOT/releases/download/v1.2.5"
create_release "$MISMATCH_DIR" v1.2.6
if PROXYMA_RELEASE_BASE_URL="$TEST_BASE_URL" PROXYMA_ALLOW_NON_LINUX_TEST=1 \
  PROXYMA_TEST_RESULT="$RESULT_FILE" \
  bash "$ROOT/scripts/download-proxyma-server-docker.sh" --version v1.2.5 >/dev/null 2>&1; then
  fail "mismatched release metadata was accepted"
fi

UNSAFE_DIR="$TEST_ROOT/releases/download/v1.2.7"
mkdir -p "$UNSAFE_DIR" "$TEST_ROOT/unsafe-source"
printf 'unsafe\n' >"$TEST_ROOT/unsafe-source/payload"
tar -C "$TEST_ROOT/unsafe-source" -czf "$UNSAFE_DIR/proxyma_docker.tar.gz" \
  --transform='s#^payload#proxyma/../escape#' payload
(
  cd "$UNSAFE_DIR"
  sha256sum proxyma_docker.tar.gz >SHA256SUMS
)
if PROXYMA_RELEASE_BASE_URL="$TEST_BASE_URL" PROXYMA_ALLOW_NON_LINUX_TEST=1 \
  PROXYMA_TEST_RESULT="$RESULT_FILE" \
  bash "$ROOT/scripts/download-proxyma-server-docker.sh" --version v1.2.7 >/dev/null 2>&1; then
  fail "archive path traversal was accepted"
fi

PACKAGE_ONE="$TEST_ROOT/package-one"
PACKAGE_TWO="$TEST_ROOT/package-two"
SOURCE_DATE_EPOCH=1700000000 bash "$ROOT/scripts/package-server-release-docker.sh" \
  v1.2.3 deadbeef "$PACKAGE_ONE" >/dev/null
SOURCE_DATE_EPOCH=1700000000 bash "$ROOT/scripts/package-server-release-docker.sh" \
  v1.2.3 deadbeef "$PACKAGE_TWO" >/dev/null
cmp "$PACKAGE_ONE/proxyma_docker.tar.gz" "$PACKAGE_TWO/proxyma_docker.tar.gz" \
  || fail "Docker release archive is not reproducible"

EXPECTED_CONTENTS=$'proxyma/\nproxyma/LICENSE\nproxyma/RELEASE\nproxyma/docker-compose.example.yml\nproxyma/scripts/\nproxyma/scripts/deployment-endpoint.sh\nproxyma/scripts/install-proxyma-server-docker.sh\nproxyma/scripts/uninstall-proxyma-server-docker.sh'
ACTUAL_CONTENTS="$(tar -tzf "$PACKAGE_ONE/proxyma_docker.tar.gz")"
[[ "$ACTUAL_CONTENTS" == "$EXPECTED_CONTENTS" ]] || fail "Docker release archive contents differ"
tar -xOf "$PACKAGE_ONE/proxyma_docker.tar.gz" proxyma/RELEASE \
  | grep -Fx 'image=ghcr.io/ascemama/proxyma:1.2.3' >/dev/null \
  || fail "Docker release metadata has the wrong image"

if bash "$ROOT/scripts/package-server-release-docker.sh" \
  v1.2.3-01 deadbeef "$TEST_ROOT/invalid-package" >/dev/null 2>&1; then
  fail "Docker package script accepted an invalid prerelease version"
fi

echo "Docker release downloader and package tests passed."
