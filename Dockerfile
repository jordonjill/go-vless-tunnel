# ── Stage 1: Build Go binary ─────────────────────────────────
FROM golang:1.21-alpine AS builder

WORKDIR /build

COPY src/go.mod src/go.sum ./
RUN go mod download

COPY src/ ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o server .

# ── Stage 2: Runtime image ───────────────────────────────────
FROM alpine:3.19

WORKDIR /app

# Install cloudflared and runtime dependencies
RUN apk add --no-cache curl ca-certificates tzdata && \
    curl -L https://github.com/cloudflare/cloudflared/releases/latest/download/cloudflared-linux-amd64 \
    -o /usr/local/bin/cloudflared \
    && chmod +x /usr/local/bin/cloudflared

COPY --from=builder /build/server /app/server
COPY start.sh /app/start.sh
RUN chmod +x /app/start.sh /app/server

CMD ["/app/start.sh"]
