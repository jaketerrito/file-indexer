# syntax=docker/dockerfile:1@sha256:87999aa3d42bdc6bea60565083ee17e86d1f3339802f543c0d03998580f9cb89
ARG BUILD_TARGET=./cmd/indexer

# Build the application from source
FROM golang:1.26.5-alpine3.24@sha256:0178a641fbb4858c5f1b48e34bdaabe0350a330a1b1149aabd498d0699ff5fb2 AS build-stage

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

# NON ROOT
USER 10001:10001

ENTRYPOINT ["/main"]
