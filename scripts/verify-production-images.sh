#!/usr/bin/env bash
set -euo pipefail

# Production image policy:
#   1. manifests must use immutable sha256 digests (no floating tags);
#   2. every referenced production image must pass cosign verification.
#
# The current v0.1 repository has no production deployment manifest directory.
# The gate therefore passes with zero images today, but becomes fail-closed as
# soon as a production manifest is added under one of the paths below.

mapfile -t files < <(
  git ls-files     'deploy/production/*.yaml' 'deploy/production/*.yml'     'deploy/production/**/*.yaml' 'deploy/production/**/*.yml'     'manifests/production/*.yaml' 'manifests/production/*.yml'     'manifests/production/**/*.yaml' 'manifests/production/**/*.yml'     'config/production/*.yaml' 'config/production/*.yml'     'config/production/**/*.yaml' 'config/production/**/*.yml'
)

if (( ${#files[@]} == 0 )); then
  echo "Supply-chain check: no production manifests yet; policy armed."
  exit 0
fi

images=()
for file in "${files[@]}"; do
  while IFS= read -r image; do
    [[ -n "$image" ]] || continue
    images+=("$image")
  done < <(
    awk '
      /^[[:space:]]*image:[[:space:]]*/ {
        value=$0
        sub(/^[[:space:]]*image:[[:space:]]*/, "", value)
        sub(/[[:space:]]*#.*/, "", value)
        gsub(/^["'"'"']|["'"'"']$/, "", value)
        print value
      }
    ' "$file"
  )
done

if (( ${#images[@]} == 0 )); then
  echo "Supply-chain check: production manifests contain no container images."
  exit 0
fi

bad=0
for image in "${images[@]}"; do
  if [[ ! "$image" =~ @sha256:[0-9a-fA-F]{64}$ ]]; then
    echo "ERROR: production image is not digest-pinned: $image" >&2
    bad=1
  fi
done
(( bad == 0 )) || exit 1

command -v cosign >/dev/null 2>&1 || {
  echo "ERROR: cosign is required when production images are present." >&2
  exit 1
}

verify_args=()
if [[ -n "${COSIGN_PUBLIC_KEY:-}" ]]; then
  verify_args+=(--key "$COSIGN_PUBLIC_KEY")
elif [[ -n "${COSIGN_CERTIFICATE_IDENTITY_REGEXP:-}" && -n "${COSIGN_CERTIFICATE_OIDC_ISSUER:-}" ]]; then
  verify_args+=(
    --certificate-identity-regexp "$COSIGN_CERTIFICATE_IDENTITY_REGEXP"
    --certificate-oidc-issuer "$COSIGN_CERTIFICATE_OIDC_ISSUER"
  )
else
  echo "ERROR: configure COSIGN_PUBLIC_KEY or keyless certificate identity + issuer." >&2
  exit 1
fi

mapfile -t unique_images < <(printf '%s\n' "${images[@]}" | sort -u)
for image in "${unique_images[@]}"; do
  echo "cosign verify $image"
  cosign verify "${verify_args[@]}" "$image" >/dev/null
done

echo "Supply-chain check PASS: ${#unique_images[@]} digest-pinned, signed production image(s)."
