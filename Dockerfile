# ── Stage 1: Build Go binary ─────────────────────────────────
FROM golang:1.27-alpine AS builder

WORKDIR /build

COPY src/go.mod src/go.sum ./
RUN go mod download

COPY src/ ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o server .

# ── Stage 2: Runtime image ───────────────────────────────────
FROM alpine:3.24

WORKDIR /app

# Install cloudflared and runtime dependencies.
# TARGETARCH (amd64, arm64, arm, 386) is set by BuildKit; the legacy builder
# leaves it empty, so fall back to amd64 as before.
ARG TARGETARCH
RUN apk add --no-cache curl ca-certificates tzdata && \
    curl -fL https://github.com/cloudflare/cloudflared/releases/latest/download/cloudflared-linux-${TARGETARCH:-amd64} \
    -o /usr/local/bin/cloudflared \
    && chmod +x /usr/local/bin/cloudflared

COPY --from=builder /build/server /app/server
COPY start.sh /app/start.sh
RUN chmod +x /app/start.sh /app/server

CMD ["/app/start.sh"]
