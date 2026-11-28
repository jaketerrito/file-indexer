# syntax=docker/dockerfile:1@sha256:572cebbc2b15e6f84c077742952674889cb63cd5e9b80abdffcbe587b6f19183
ARG BUILD_TARGET=./cmd/index-stat

# Build the application from source
FROM golang:1.27.4-alpine3.24@sha256:70bd7ddea8b6cbe5d6943cde279d6b67feb10cc924fb9100d1d8757c1575b280 AS build-stage

ARG BUILD_TARGET
WORKDIR /app

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY . .

# Build the static binary using build mounts for the Go cache
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /main ${BUILD_TARGET}


FROM build-stage AS run-test-stage
RUN --mount=type=cache,target=/root/.cache/go-build \
    go test -v ./...


# Deploy the application binary into a lean image
FROM scratch AS build-release-stage

WORKDIR /

COPY --from=build-stage /main /main
# CA bundle for outbound TLS (hosted S3 providers with S3_SECURE=true);
# scratch ships none, so x509 verification fails without it.
COPY --from=build-stage /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=ghcr.io/grpc-ecosystem/grpc-health-probe@sha256:55a379bb4a7ef1c779e4e123ff31dd0046d1014eab083a89f4ac196a97548bec /ko-app/grpc-health-probe /bin/grpc_health_probe

# NON ROOT
USER 10001:10001

ENTRYPOINT ["/main"]
