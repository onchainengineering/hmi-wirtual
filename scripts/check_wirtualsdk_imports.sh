#!/usr/bin/env bash

# This file checks all wirtualsdk imports to be sure it doesn't import any packages
# that are being replaced in go.mod.

set -euo pipefail
# shellcheck source=scripts/lib.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
cdroot

deps=$(./scripts/list_dependencies.sh github.com/onchainengineering/hmi-wirtual/wirtualsdk)

set +e
replaces=$(grep "^replace" go.mod | awk '{print $2}')
conflicts=$(echo "$deps" | grep -xF -f <(echo "$replaces"))

if [ -n "${conflicts}" ]; then
	error "$(printf 'wirtualsdk cannot import the following packages being replaced in go.mod:\n%s' "${conflicts}")"
fi
log "wirtualsdk imports OK"
