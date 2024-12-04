#!/usr/bin/env bash

set -euo pipefail

if [[ $# -lt 1 ]]; then
	echo "Usage: $0 <coder URL>"
	exit 1
fi

# Allow toggling verbose output
[[ -n ${VERBOSE:-} ]] && set -x

WIRTUAL_URL=$1
DRY_RUN="${DRY_RUN:-0}"
PROJECT_ROOT="$(git rev-parse --show-toplevel)"
# shellcheck source=scripts/lib.sh
source "${PROJECT_ROOT}/scripts/lib.sh"
CONFIG_DIR="${PROJECT_ROOT}/scaletest/.coderv2"
ARCH="$(arch)"
if [[ "$ARCH" == "x86_64" ]]; then
	ARCH="amd64"
fi

if [[ -f "${CONFIG_DIR}/coder.env" ]]; then
	echo "Found existing coder.env in ${CONFIG_DIR}!"
	echo "Nothing to do, exiting."
	exit 0
fi

maybedryrun "$DRY_RUN" mkdir -p "${CONFIG_DIR}"
echo "Fetching Coder for first-time setup!"
pod=$(kubectl get pods \
	--namespace="${NAMESPACE}" \
	--selector="app.kubernetes.io/name=coder,app.kubernetes.io/part-of=coder" \
	--output="jsonpath='{.items[0].metadata.name}'")
if [[ -z ${pod} ]]; then
	log "Could not find coder pod!"
	exit 1
fi
maybedryrun "$DRY_RUN" kubectl \
	--namespace="${NAMESPACE}" \
	cp \
	--container=coder \
	"${pod}:/opt/coder" "${CONFIG_DIR}/coder"
maybedryrun "$DRY_RUN" chmod +x "${CONFIG_DIR}/coder"

set +o pipefail
RANDOM_ADMIN_PASSWORD=$(tr </dev/urandom -dc _A-Z-a-z-0-9 | head -c16)
set -o pipefail
WIRTUAL_FIRST_USER_EMAIL="admin@coder.com"
WIRTUAL_FIRST_USER_USERNAME="coder"
WIRTUAL_FIRST_USER_PASSWORD="${RANDOM_ADMIN_PASSWORD}"
WIRTUAL_FIRST_USER_TRIAL="false"
echo "Running login command!"
DRY_RUN="$DRY_RUN" "${PROJECT_ROOT}/scaletest/lib/coder_shim.sh" login "${WIRTUAL_URL}" \
	--global-config="${CONFIG_DIR}" \
	--first-user-username="${WIRTUAL_FIRST_USER_USERNAME}" \
	--first-user-email="${WIRTUAL_FIRST_USER_EMAIL}" \
	--first-user-password="${WIRTUAL_FIRST_USER_PASSWORD}" \
	--first-user-trial=false

echo "Writing credentials to ${CONFIG_DIR}/coder.env"
maybedryrun "$DRY_RUN" cat <<EOF >"${CONFIG_DIR}/coder.env"
WIRTUAL_FIRST_USER_EMAIL=admin@coder.com
WIRTUAL_FIRST_USER_USERNAME=coder
WIRTUAL_FIRST_USER_PASSWORD="${RANDOM_ADMIN_PASSWORD}"
WIRTUAL_FIRST_USER_TRIAL="${WIRTUAL_FIRST_USER_TRIAL}"
EOF

echo "Importing kubernetes template"
DRY_RUN="$DRY_RUN" "$PROJECT_ROOT/scaletest/lib/coder_shim.sh" templates push \
	--global-config="${CONFIG_DIR}" \
	--directory "${CONFIG_DIR}/templates/kubernetes" \
	--yes kubernetes
