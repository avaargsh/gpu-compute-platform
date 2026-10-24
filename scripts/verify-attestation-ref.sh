#!/usr/bin/env bash
set -euo pipefail

event_name="${1:-}"
source_ref="${2:-}"

# Manual attestations are allowed only from protected master. Version tags are
# the release path regardless of whether the tag event is push or dispatch.
if [[ "$source_ref" == refs/tags/v* ]]; then
  exit 0
fi
if [[ "$event_name" == "workflow_dispatch" && "$source_ref" == "refs/heads/master" ]]; then
  exit 0
fi

printf 'ERROR: release attestations require a v* tag or workflow_dispatch on refs/heads/master (event=%s ref=%s)\n' \
  "$event_name" "$source_ref" >&2
exit 1
