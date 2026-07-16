#!/usr/bin/env bash
set -euo pipefail

binary=$1
version_file=$2
want=$(tr -d '[:space:]' < "$version_file")
got=$("$binary" -version)
[[ $got == "$want" ]] || {
    echo "provider version is '$got', want '$want'" >&2
    exit 1
}
