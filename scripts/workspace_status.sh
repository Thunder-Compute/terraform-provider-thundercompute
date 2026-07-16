#!/bin/sh
set -eu

version_file="terraform/VERSION"
if [ ! -f "$version_file" ]; then
    version_file="VERSION"
fi

version=$(tr -d '[:space:]' < "$version_file")
case "$version" in
    ''|*[!0-9A-Za-z.+-]*)
        echo "invalid Terraform provider version in $version_file: $version" >&2
        exit 1
        ;;
esac

printf 'STABLE_TERRAFORM_VERSION %s\n' "$version"
