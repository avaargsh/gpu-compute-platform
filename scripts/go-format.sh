#!/usr/bin/env bash
set -euo pipefail

mapfile -d '' files < <(find cmd internal -name '*.go' -type f -print0 | sort -z)
if ((${#files[@]} == 0)); then
  exit 0
fi

if [[ "${1:-}" == "--check" ]]; then
  unformatted="$(printf '%s\\0' "${files[@]}" | xargs -0 gofmt -l)"
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

printf '%s\\0' "${files[@]}" | xargs -0 gofmt -w
