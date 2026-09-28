FROM golang:1.24 AS build
WORKDIR /src

COPY go.mod ./
RUN go mod download

COPY cmd ./cmd
COPY internal ./internal

ARG TARGETOS=linux
ARG TARGETARCH=amd64
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH     go build -trimpath -o /out/control-plane ./cmd/control-plane &&     CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH     go build -trimpath -o /out/cluster-agent ./cmd/cluster-agent

FROM gcr.io/distroless/static-debian12:nonroot AS control-plane
COPY --from=build /out/control-plane /control-plane
EXPOSE 8080
ENTRYPOINT ["/control-plane"]

FROM gcr.io/distroless/static-debian12:nonroot AS cluster-agent
COPY --from=build /out/cluster-agent /cluster-agent
ENTRYPOINT ["/cluster-agent"]
