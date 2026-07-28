#!/usr/bin/env bash
set -euo pipefail

usage() {
  echo "Usage: $0 <image-name> <vMAJOR.MINOR.PATCH[-PRERELEASE]>" >&2
}

if [[ $# -ne 2 || -z "$1" || -z "$2" ]]; then
  usage
  exit 2
fi

image="$1"
git_tag="$2"

if [[ ! "$git_tag" =~ ^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-([0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*))?$ ]]; then
  echo "Invalid release tag: $git_tag" >&2
  echo "Expected vMAJOR.MINOR.PATCH or vMAJOR.MINOR.PATCH-PRERELEASE without build metadata." >&2
  exit 1
fi

major="${BASH_REMATCH[1]}"
minor="${BASH_REMATCH[2]}"
patch="${BASH_REMATCH[3]}"
prerelease="${BASH_REMATCH[4]}"
version="$major.$minor.$patch$prerelease"

if [[ -n "$prerelease" ]]; then
  IFS='.' read -r -a identifiers <<<"${prerelease#-}"
  for identifier in "${identifiers[@]}"; do
    if [[ "$identifier" =~ ^[0-9]+$ && ${#identifier} -gt 1 && "$identifier" == 0* ]]; then
      echo "Invalid numeric prerelease identifier with a leading zero: $identifier" >&2
      exit 1
    fi
  done
  printf '%s:%s\n' "$image" "$version"
  exit 0
fi

printf '%s:%s\n' "$image" "$version"
printf '%s:%s.%s\n' "$image" "$major" "$minor"
printf '%s:%s\n' "$image" "$major"
printf '%s:latest\n' "$image"
