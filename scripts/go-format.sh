#!/usr/bin/env bash
set -euo pipefail

if [[ "${1:-}" == "--check" ]]; then
  unformatted="$(find cmd internal -name '*.go' -type f -print0 | sort -z | xargs -0 -r gofmt -l)"
  if [[ -n "$unformatted" ]]; then
    echo "gofmt required for:"
    echo "$unformatted"
    echo
    while IFS= read -r file; do
      [[ -z "$file" ]] || gofmt -d "$file"
    done <<< "$unformatted"
    exit 1
  fi
  exit 0
fi

find cmd internal -name '*.go' -type f -print0 | sort -z | xargs -0 -r gofmt -w
