# Management API image — isolated local delivery gate

**Status: development/acceptance artifact, not an approved public
production deployment.** The repository's canonical `Dockerfile` now
publishes an explicit `management-api` target alongside `control-plane`
and `cluster-agent`. The default image remains the control plane.

## Build and local inspection

```bash
docker build -f Dockerfile --target management-api -t gpu-management-api:local .
bash scripts/e2e/management-image-security-smoke.sh gpu-management-api:local
```

The runtime is `gcr.io/distroless/static-debian12:nonroot` with only the
Go management binary as entrypoint. It **must** run as nonroot and be
compatible with `--read-only --cap-drop ALL --security-opt
no-new-privileges`. No shell, package manager or embedded browser UI
belongs inside this image. Do not bake DB URLs, token secrets or TLS keys into
the image or commit them into the repository.

Default HTTP listener: **127.0.0.1:8081 inside its own network namespace**.
With a standard Docker bridge or ordinary Kubernetes Pod this is **not**
reachable through a published port/Service; publishing `-p 8081:8081`
does **not** change the bind. This is intentional fail-closed behavior.
In a single-host **disposable Linux acceptance fixture**, `--network host`
permits a local caller and a local DB test. This is not an approved
production distribution topology.

Do **not** set `MANAGEMENT_LISTEN_ADDR=0.0.0.0:8081` to make a service
reachable; the process correctly refuses the configuration. Do **not** put
an unauthenticated proxy or gateway in front of it. A reviewed
same-origin TLS ingress, restricted network boundary and correct
external browser origin support are separate work, not implied by an image
build.

## Required configuration and trust boundaries

The binary requires:
- `MANAGEMENT_DATABASE_URL`: PostgreSQL URI using
  `sslmode=verify-full&sslrootcert=<absolute readable CA path>`, with
  a **separate non-owner, non-superuser LOGIN** that inherits only
  `gpu_management_runtime`; provision via DBA-controlled migration, not
  via the application;
- `OIDC_ISSUER`, `OIDC_INTROSPECTION_URL`, `OIDC_AUDIENCE`,
  `OIDC_CLIENT_ID`, `OIDC_CLIENT_SECRET`: real independent IdP trust and
  confidential introspection credentials available to the server only;
- `MANAGEMENT_LISTEN_ADDR` default `127.0.0.1:8081`;
  `MANAGEMENT_BROWSER_ORIGIN` may only be an exact
  `http://127.0.0.1:<port>` local pilot browser Origin.

Do not pass production secrets through CI, terminal command lines,
plaintext Docker environment files or Git. Any future deployment must
adopt an external secrets system and audited injection method; no example
in this document is a ready-to-run production compose manifest.

## CI evidence and limitations

`.github/workflows/go-control-plane.yml` builds the real
`management-api` image and runs the negative container smoke test:
nonroot identity, canonical entrypoint, read-only filesystem, empty
database configuration refusal, weak TLS rejection and invalid wildcard
IPv4/IPv6 bind refusal.

`.github/workflows/management-security-integration.yml` runs a
**positive** disposable PostgreSQL 17 acceptance test. It builds and
starts the actual management image with `--network host --read-only`
on the CI Linux runner, explicitly limited capabilities, a projected
test-only CA and the minimally privileged PostgreSQL LOGIN. The real
database's `pg_stat_ssl` proves an encrypted runtime session.
The same workflow independently checks CA replacement/rollback and
incorrect server CA / DNS hostname / TLS downgrade refusal.

Passing these proves container packaging and a restricted *local*
management runtime, **not** production ingress, backup/restore, real
Kubernetes workload execution, full Agent mTLS production rollout, OTel,
SLO, secrets rotation or multi-tenant network policies. Production
manifest publication remains subject to digest-pinning and cosign
verification via `scripts/verify-production-images.sh`.
