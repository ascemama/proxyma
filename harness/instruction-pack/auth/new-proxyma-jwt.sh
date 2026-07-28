#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
config_path="${1:-"$script_dir/../../agent-configuration/df9ec8bb-1a22-4181-a7c8-126ad553221d.json"}"

python3 - "$config_path" <<'PY'
import base64
import hashlib
import hmac
import json
import sys
import time

config_path = sys.argv[1]
with open(config_path, "r", encoding="utf-8") as handle:
    config = json.load(handle)

agent_id = config.get("agent_id", "").strip()
agent_jwt_signing_key = config.get("agent_jwt_signing_key", "")
if not agent_id or not agent_jwt_signing_key:
    raise SystemExit("Agent config must include agent_id and agent_jwt_signing_key.")

def b64url(data):
    return base64.urlsafe_b64encode(data).rstrip(b"=").decode("ascii")

now = int(time.time())
header = {"alg": "HS256", "typ": "JWT", "kid": agent_id}
payload = {"sub": agent_id, "iat": now, "exp": now + 600}
encoded_header = b64url(json.dumps(header, separators=(",", ":")).encode("utf-8"))
encoded_payload = b64url(json.dumps(payload, separators=(",", ":")).encode("utf-8"))
signing_input = f"{encoded_header}.{encoded_payload}"
signature = hmac.new(agent_jwt_signing_key.encode("utf-8"), signing_input.encode("ascii"), hashlib.sha256).digest()
print(f"{signing_input}.{b64url(signature)}")
PY
