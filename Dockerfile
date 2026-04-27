# syntax=docker/dockerfile:1
# Dockerfile for Clonarr
#
# Architecture: Multi-stage build (Builder -> Runtime)
# 
# Environment Variables:
#   - PUID : User ID for file permissions mapping (default: 99 - nobody)
#   - PGID : Group ID for file permissions mapping (default: 100 - users)
#   - PORT : The port the Clonarr web interface listens on (default: 6060)
#
# Volumes:
#   - /config : Persistent storage for Clonarr's database and configuration

FROM golang:1.25-alpine AS builder

ARG VERSION=2.2.4

RUN apk add --no-cache git

WORKDIR /build
# Copy dependency files first for layer caching
COPY go.mod go.sum ./

# Use cache mounts for go modules to speed up subsequent builds
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

# Copy the remaining application source code
COPY . .

# Build the application binary.
# Uses cache mounts for Go build cache and explicitly sets GOOS=linux for cross-compilation compatibility.
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w -X main.Version=${VERSION}" -o clonarr .

FROM alpine:3.21

ARG VERSION=2.2.4

# Open Container Initiative (OCI) labels for registry metadata
LABEL org.opencontainers.image.version=${VERSION} \
      org.opencontainers.image.title="clonarr" \
      org.opencontainers.image.description="Clonarr application" \
      org.opencontainers.image.source="https://github.com/ProphetSe7en/clonarr"

# Install runtime dependencies (git, tini, timezone data, certs) and su-exec for stepping down from root.
# Also ensures the nobody:users combination exists for permission mapping.
RUN apk add --no-cache git tini tzdata ca-certificates su-exec && \
    addgroup -g 100 users 2>/dev/null || true && \
    adduser -D -u 99 -G users nobody 2>/dev/null || true && \
    mkdir -p /config && \
    chown -R nobody:users /config

# Copy the compiled binary from the builder stage
COPY --from=builder /build/clonarr /usr/local/bin/clonarr

# Copy the startup script (using --chmod to avoid an extra RUN layer setting permissions)
COPY --chmod=755 entrypoint.sh /entrypoint.sh

# Define the volume for persistent application data
VOLUME /config

# Default environment variables for permission handling and web port
ENV PUID=99 \
    PGID=100 \
    PORT=6060

# Expose the application web port
EXPOSE 6060

# Healthcheck to ensure the container is routing traffic properly
HEALTHCHECK --interval=30s --timeout=5s --retries=3 \
  CMD sh -c 'wget -qO- "http://localhost:${PORT}/api/health" || exit 1'

# Use tini as the init system to handle process reaping and signal forwarding
ENTRYPOINT ["/sbin/tini", "--", "/entrypoint.sh"]