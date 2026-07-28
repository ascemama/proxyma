#!/usr/bin/env bash

validate_bind_address() {
  local address="$1"

  if [[ -z "$address" ]]; then
    echo "Bind address must not be empty." >&2
    return 1
  fi

  if [[ "$address" =~ ^\[[A-Za-z0-9:.%_-]+\]$ ]]; then
    return 0
  fi

  if [[ "$address" =~ ^[A-Za-z0-9]([A-Za-z0-9.-]*[A-Za-z0-9])?$ ]]; then
    return 0
  fi

  echo "Invalid bind address: $address" >&2
  echo "Use an IPv4 address, hostname, or bracketed IPv6 address such as [::]." >&2
  return 1
}

validate_port() {
  local label="$1"
  local port="$2"

  if [[ ! "$port" =~ ^[0-9]{1,5}$ ]] || (( 10#$port < 1 || 10#$port > 65535 )); then
    echo "$label must be a numeric value from 1 through 65535: $port" >&2
    return 1
  fi
}

format_listen_address() {
  printf '%s:%s' "$1" "$2"
}

parse_listen_address() {
  local listen_address="$1"

  PARSED_BIND_ADDRESS=""
  PARSED_PORT=""
  if [[ "$listen_address" =~ ^(\[[^]]+\]):([0-9]+)$ ]]; then
    PARSED_BIND_ADDRESS="${BASH_REMATCH[1]}"
    PARSED_PORT="${BASH_REMATCH[2]}"
  elif [[ "$listen_address" =~ ^([^:]*):([0-9]+)$ ]]; then
    PARSED_BIND_ADDRESS="${BASH_REMATCH[1]}"
    PARSED_PORT="${BASH_REMATCH[2]}"
  else
    echo "Invalid existing listen_address: $listen_address" >&2
    return 1
  fi

  if [[ -z "$PARSED_BIND_ADDRESS" ]]; then
    PARSED_BIND_ADDRESS="0.0.0.0"
  fi
  validate_bind_address "$PARSED_BIND_ADDRESS"
  validate_port "Listen port" "$PARSED_PORT"
}

read_json_listen_address() {
  local config_file="$1"
  local matches=()

  mapfile -t matches < <(sed -nE 's/.*"listen_address"[[:space:]]*:[[:space:]]*"([^"]*)".*/\1/p' "$config_file")
  if [[ "${#matches[@]}" -ne 1 ]]; then
    echo "Expected exactly one server.listen_address string in $config_file; found ${#matches[@]}." >&2
    return 1
  fi
  printf '%s' "${matches[0]}"
}

update_json_listen_address() {
  local config_file="$1"
  local listen_address="$2"
  local config_dir temp_file

  config_dir="$(dirname "$config_file")"
  temp_file="$(mktemp "$config_dir/.proxyma.config.XXXXXX")"
  if ! awk -v replacement="\"listen_address\": \"$listen_address\"" '
    !updated {
      start = match($0, /"listen_address"[[:space:]]*:[[:space:]]*"[^"]*"/)
      if (start != 0) {
        $0 = substr($0, 1, start - 1) replacement substr($0, start + RLENGTH)
        updated = 1
      }
    }
    { print }
    END { if (!updated) exit 42 }
  ' "$config_file" > "$temp_file"; then
    rm -f "$temp_file"
    echo "Could not update server.listen_address in $config_file." >&2
    return 1
  fi
  chmod --reference="$config_file" "$temp_file"
  chown --reference="$config_file" "$temp_file"
  mv -f "$temp_file" "$config_file"
}
