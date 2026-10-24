# Security, SBOM and build provenance acceptance

Status: **Go source and shipped binary gates are enforced in CI. Production container-image acceptance remains explicitly blocked until a real image publication/deployment path is configured.**

## Pull-request gates

The dedicated `Go Security and Supply Chain` workflow runs on every pull request and performs:

1. A strict `go mod tidy` check against checked-in `go.mod`/`go.sum` using Go `1.27.3`.
2. Reachability-aware Go vulnerability analysis with pinned `govulncheck v1.9.0`. Any untriaged reachable finding fails the workflow; no default allowlist suppresses findings.
3. `make supply-chain-check`, which validates immutable SHA-256 image references and verifies cosign signatures for any production images found by its configured manifest scan.
4. Linux/amd64 builds of Control Plane, Cluster Agent, Management API and Observer.
5. One CycloneDX JSON SBOM per binary, a SHA-256 manifest, and 14-day artifact upload.

The repository-wide Go test and PostgreSQL lifecycle tests remain in the canonical Go Control Plane workflow. `go.mod`, workflow setup steps and builder Dockerfiles are pinned consistently to Go `1.27.3`.

## Release/manual provenance

For a `v*` tag or explicit `workflow_dispatch`, a separate job downloads the exact artifact from the same workflow run, verifies `SHA256SUMS`, and creates GitHub Artifact Attestations for the four binary digests (build provenance) and each binary's own SBOM. Attestation permissions are scoped to this dedicated job; ordinary PR verification retains read-only repository permissions.

A green pull-request run proves source vulnerability scanning, binary compilation, SBOM generation and checksum integrity. It does not claim that a signed release attestation was created for the PR itself.

## Explicit current limits

- The configured production-manifest scan currently finds no production deployment manifests. A passing `make supply-chain-check` therefore means the policy is armed but evaluated zero production images; it does not prove any deployed image was verified.
- This workflow does not publish container images to a registry. Digest pinning, final image-layer CVE scanning, cosign verification against a real production trust identity, and registry-side provenance remain blocked until publication/deployment is configured.
- SBOMs cover Go binary dependency inventories. They do not replace scanning the final container OS layers or validating runtime configuration.
- OIDC tenant isolation, authorization revocation, Provider UID/owner-chain checks, and real lost-ACK/controller-crash scenarios require their own integration evidence; `govulncheck` is not a substitute.

## Release evidence checklist

For a release candidate, preserve the exact source SHA, workflow run URL, binary artifacts, per-binary CycloneDX files, checksums, and signed attestations. Check each attested subject digest against the artifact actually shipped. Do not mark production image supply-chain acceptance complete until production manifests exist and all referenced images are digest-pinned, scanned, and verified against the configured trusted signing identity.
