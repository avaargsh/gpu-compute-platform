# Canonical AI Compute Control Plane container build.
# The Go control plane and cluster agent are the supported runtime entrypoints.
# Accelerator runtimes stay on Kubernetes nodes/providers; they are not baked
# into the management-plane image.

FROM golang:1.24 AS build
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY cmd ./cmd
COPY internal ./internal

ARG TARGETOS=linux
ARG TARGETARCH=amd64
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -o /out/control-plane ./cmd/control-plane && \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -o /out/cluster-agent ./cmd/cluster-agent && \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -o /out/management-api ./cmd/management-api && \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -o /out/verifiedops-observer ./cmd/verifiedops-observer

FROM gcr.io/distroless/static-debian12:nonroot AS control-plane
COPY --from=build /out/control-plane /control-plane
EXPOSE 8080
ENTRYPOINT ["/control-plane"]

FROM gcr.io/distroless/static-debian12:nonroot AS cluster-agent
COPY --from=build /out/cluster-agent /cluster-agent
ENTRYPOINT ["/cluster-agent"]

# Independent human Management API: OIDC introspection + Postgres RBAC/CAS.
# Default process only accepts loopback on 8081; it is intentionally NOT a
# remotely exposed Kubernetes deployment or a replacement for TLS ingress.
FROM gcr.io/distroless/static-debian12:nonroot AS management-api
COPY --from=build /out/management-api /management-api
EXPOSE 8081
ENTRYPOINT ["/management-api"]

# Standalone one-shot Observer process. This is not the Management API or
# Agent image; it has no shell, KUBECONFIG, key material or bundled tickets.
# The reviewed Kubernetes example remains suspended until operator approval.
FROM gcr.io/distroless/static-debian12:nonroot AS verifiedops-observer
COPY --from=build /out/verifiedops-observer /verifiedops-observer
ENTRYPOINT ["/verifiedops-observer"]

# TEST ONLY: busybox is required by the isolated kind fixture's initContainer
# to stage ephemeral, Kubernetes Secret-backed credentials into regular files.
# Production must use the distroless target above + reviewed external signer.
FROM busybox:1.36 AS verifiedops-observer-e2e
COPY --from=build /out/verifiedops-observer /verifiedops-observer
USER 65532:65532
ENTRYPOINT ["/verifiedops-observer"]

# Keep plain "docker build ." intuitive: it produces the control-plane image.
FROM control-plane AS default
