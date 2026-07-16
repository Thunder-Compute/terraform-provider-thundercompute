#!/usr/bin/env bash

terraform_bazel_workspace_init() {
    local force_standalone=${1:-0}
    local script_dir source_parent

    script_dir=$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
    TERRAFORM_SOURCE_DIR=$(CDPATH= cd -- "$script_dir/.." && pwd)
    source_parent=$(dirname -- "$TERRAFORM_SOURCE_DIR")

    TERRAFORM_BAZEL_TEMP=
    if [[ $force_standalone != 1 && -f "$source_parent/MODULE.bazel" && "$TERRAFORM_SOURCE_DIR" == "$source_parent/terraform" ]]; then
        TERRAFORM_BAZEL_WORKSPACE=$source_parent
        return
    fi

    TERRAFORM_BAZEL_TEMP=$(mktemp -d "${TMPDIR:-/tmp}/thunder-terraform-bazel.XXXXXX")
    TERRAFORM_BAZEL_WORKSPACE=$TERRAFORM_BAZEL_TEMP
    mkdir -p "$TERRAFORM_BAZEL_WORKSPACE/terraform"

    (
        cd "$TERRAFORM_SOURCE_DIR"
        tar \
            --exclude='./.git' \
            --exclude='./dist' \
            --exclude='./terraform-provider-thundercompute_v*' \
            -cf - .
    ) | (
        cd "$TERRAFORM_BAZEL_WORKSPACE/terraform"
        tar -xf -
    )

    cp "$TERRAFORM_SOURCE_DIR/release/bazel/MODULE.bazel" "$TERRAFORM_BAZEL_WORKSPACE/MODULE.bazel"
    cp "$TERRAFORM_SOURCE_DIR/release/bazel/.bazelrc" "$TERRAFORM_BAZEL_WORKSPACE/.bazelrc"
    cp "$TERRAFORM_SOURCE_DIR/.bazelversion" "$TERRAFORM_BAZEL_WORKSPACE/.bazelversion"
    if [[ -f "$TERRAFORM_SOURCE_DIR/release/bazel/MODULE.bazel.lock" ]]; then
        cp "$TERRAFORM_SOURCE_DIR/release/bazel/MODULE.bazel.lock" "$TERRAFORM_BAZEL_WORKSPACE/MODULE.bazel.lock"
    fi
}

terraform_bazel_workspace_cleanup() {
    if [[ -n ${TERRAFORM_BAZEL_TEMP:-} && -d $TERRAFORM_BAZEL_TEMP ]]; then
        rm -rf -- "$TERRAFORM_BAZEL_TEMP"
    fi
}

terraform_bazel() {
    (
        cd "$TERRAFORM_BAZEL_WORKSPACE"
        "${BAZEL:-bazel}" "$@"
    )
}
