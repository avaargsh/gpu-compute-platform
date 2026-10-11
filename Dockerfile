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
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -o /out/management-api ./cmd/management-api

FROM gcr.io/distroless/static-debian12:nonroot AS control-plane
COPY --from=build /out/control-plane /control-plane
EXPOSE 8080
ENTRYPOINT ["/control-plane"]

FROM gcr.io/distroless/static-debian12:nonroot AS cluster-agent
COPY --from=build /out/cluster-agent /cluster-agent
ENTRYPOINT ["/cluster-agent"]

# Dedicated management-plane process: only authenticated routes, no
# legacy Agent API. Bound to loopback; reverse proxy MUST be a same-Pod
# sidecar with shared network namespace if remotely accessed at all.
FROM gcr.io/distroless/static-debian12:nonroot AS management-api
COPY --from=build /out/management-api /management-api
EXPOSE 8081
ENTRYPOINT ["/management-api"]

# Keep plain "docker build ." intuitive: it produces the control-plane image.
FROM control-plane AS default
