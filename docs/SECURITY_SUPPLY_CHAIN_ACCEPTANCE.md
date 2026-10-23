# Security, SBOM and build provenance acceptance

Status: **enforced checks for Go source and binary artifacts; production image acceptance remains gated on a real image publication path**.

## Pull-request gates

The canonical Go Control Plane workflow runs these checks for pull requests on the exact candidate SHA:

1. Go `govulncheck` source analysis with tool version `v1.9.0`, using the repository's pinned Go `1.27.3` toolchain. Reachable vulnerability findings fail the job; do not replace a failure with an untracked allowlist.
2. The PostgreSQL-backed lifecycle/recovery subset in `make acceptance-contract`, independently of the full unit suite.
3. `make supply-chain-check`. This parses production manifests structurally, requires immutable SHA-256 image digests, and verifies cosign signatures when production images are declared.
4. Reproducible-shape Linux/amd64 builds of Control Plane, Cluster Agent, Management API and Observer, plus one CycloneDX JSON SBOM per binary and a SHA-256 checksums file. Binary/SBOM artifacts are uploaded for 14 days from each workflow run.

The tool versions are pinned: govulncheck `v1.9.0` and cyclonedx-gomod `v1.12.0`. The Go toolchain version is pinned consistently across `go.mod`, CI and builder containers (`1.27.3`).

## Release/manual provenance

For a `v*` tag or explicit `workflow_dispatch`, the workflow creates GitHub Artifact Attestations for the four built binaries (SLSA build provenance) and their per-binary CycloneDX SBOMs. The attestations describe the actual workflow-built file digests and are discoverable through the repository's attestations UI / GitHub CLI. The artifact files and checksums are also retained as workflow artifacts.

Attestation steps intentionally do not run for ordinary pull requests, where the event may be from a fork and cannot receive write-scoped attestation permissions. A green PR means the artifacts and SBOMs were generated and the security checks passed; it does not mean a signed release attestation was created for that PR.

## Explicit current limits

- There are currently no production deployment manifests under the directories scanned by `scripts/verify-production-images.sh`. Its green result therefore means *policy armed, zero production images checked*, not that deployed images were signature-verified.
- The current workflow builds and smoke-tests container images only on manual/tag runs. It does not push those images to a declared registry, so container-image provenance and digest-pinned deployment cannot yet be accepted.
- This gate produces Go dependency SBOMs for each binary. It does not yet scan the final distroless image layers for OS-package vulnerabilities or prove container-registry identity.
- Tenant isolation, token revocation/expiry, credential broker denial and quota enforcement need their own authenticated security-integration evidence; Go vulnerability scanning is not a substitute.
- A successful contract test is not equivalent to live kind/Kueue lost-ACK, provider-ownership negative tests, or controller-crash recovery. Those remain separate acceptance records.

## Release evidence checklist

For any candidate release, record the exact commit SHA and workflow run; retain the four binary digests, CycloneDX files, checksums and GitHub attestations. Verify the attested subject digest matches the artifact actually downloaded. Never mark the supply-chain gate complete until production manifests exist and each referenced image is digest-pinned, scanned and cosign-verified using the configured trusted identity.
