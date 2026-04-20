FROM golang:1.25-alpine AS builder

ARG VERSION=2.1.0

RUN apk add --no-cache git

WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w -X main.Version=${VERSION}" -o clonarr .

FROM alpine:3.21

ARG VERSION=2.1.0
LABEL org.opencontainers.image.version=${VERSION}

RUN apk add --no-cache git tini tzdata ca-certificates su-exec && \
    addgroup -g 100 users 2>/dev/null || true && \
    adduser -D -u 99 -G users nobody 2>/dev/null || true && \
    mkdir -p /config && \
    chown -R nobody:users /config

COPY --from=builder /build/clonarr /usr/local/bin/clonarr
COPY entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh

VOLUME /config

ENV PUID=99
ENV PGID=100
ENV PORT=6060
EXPOSE 6060

HEALTHCHECK --interval=30s --timeout=5s --retries=3 \
  CMD wget -qO- http://localhost:6060/api/health || exit 1

ENTRYPOINT ["/sbin/tini", "--", "/entrypoint.sh"]
