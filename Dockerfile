# syntax=docker/dockerfile:1
ARG BUILD_TARGET=./cmd/indexer

# Build the application from source
FROM golang:1.26.4-alpine AS build-stage

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
