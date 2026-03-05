# Work Tunnel VLESS

A Dockerized VLESS proxy server written in Go, integrated with Cloudflare Tunnel — a simple, secure, one-command deployment for developers.

## Features

- VLESS protocol over WebSocket
- Automatic validation of UUID, port, and domain format
- Fully environment-variable driven configuration
- Multi-stage Docker build (Go builder + Alpine runtime)
- Cloudflare Tunnel handles TLS termination — no certificate management required

## Use Cases

- **Fixed-IP access**: Route traffic through a stable server IP to bypass IP restrictions with an encrypted tunnel.
- **Remote work**: Give distributed team members a secure network channel with guaranteed data-in-transit protection.
- **Cross-region access**: Overcome geographic restrictions with a stable, low-latency connection.
- **Development & testing**: Provide a safe environment for cross-region application testing.
- **Data sync**: Protect data in transit during cross-region synchronization operations.
- **Corporate intranet access**: Give remote employees secure access to internal resources.

## Requirements

- [Docker](https://docs.docker.com/get-docker/) (with Compose plugin or `docker-compose`)
- A Cloudflare account with Zero Trust enabled and a Tunnel Token

## Cloudflare Zero Trust Tunnel Setup

1. **Log in to the Cloudflare Zero Trust dashboard**
   - Go to <https://dash.cloudflare.com/> and select **Zero Trust**.

2. **Create a Tunnel**
   - In the left sidebar choose **Access → Tunnels**.
   - Click **Create a tunnel**, enter a name (e.g. `go-vless-tunnel`), select **Docker** as the connector type, and copy the generated **Tunnel Token**.

3. **Configure a Public Hostname**
   - Open the tunnel's **Configure** page and select the **Public Hostname** tab.
   - Click **Add a public hostname**, enter your domain (e.g. `tunnel.yourdomain.com`), set the service type to **HTTP**, and set the URL to `http://localhost:3000` (or whatever `PORT` you configure).

4. **Save the values you will need**
   - Tunnel Token — used by `cloudflared` to authenticate.
   - Public hostname (domain) — used to build the VLESS URL.

## Quick Start

### 1. Clone the repository

```bash
git clone https://github.com/playwjj/go-vless-tunnel.git
cd go-vless-tunnel
```

### 2. Create an `.env` file

```bash
# Required
UUID=xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx
TUNNEL_DOMAIN=tunnel.yourdomain.com
TUNNEL_TOKEN=your_cloudflare_tunnel_token

# Optional (defaults shown)
PORT=3000
NAME=my-server
VLESS_PATH_PREFIX=/work-tunnel-
```

### 3. Run with Docker Compose

The repository ships with a ready-to-use `docker-compose.yml` that builds the image from source:

```bash
docker-compose up -d
```

### 4. Run with plain Docker

Build and start manually if you prefer not to use Compose:

```bash
docker build -t go-vless-tunnel .
docker run -d \
  --name go-vless-tunnel \
  --env-file .env \
  go-vless-tunnel
```

## Configuration

| Variable | Required | Default | Description |
|---|---|---|---|
| `UUID` | Yes | — | VLESS UUID (standard hyphenated format) |
| `TUNNEL_DOMAIN` | Yes | — | Public hostname configured in Cloudflare Tunnel |
| `TUNNEL_TOKEN` | Yes | — | Cloudflare Tunnel authentication token |
| `PORT` | No | `3000` | Port the Go server listens on inside the container |
| `NAME` | No | hostname | Label embedded in the VLESS path |
| `VLESS_PATH_PREFIX` | No | `/work-tunnel-` | WebSocket path prefix used in the VLESS URL |

## Retrieving Your VLESS Link

Once the container is running, open the following URL in a browser or `curl` it:

```
https://<TUNNEL_DOMAIN>/<UUID>
```

The server returns a ready-to-use VLESS connection string that you can paste directly into your proxy client.

## Project Structure

```
.
├── Dockerfile          # Multi-stage build: Go builder + Alpine runtime
├── docker-compose.yml  # Compose file (builds from source)
├── start.sh            # Container entrypoint — starts Go server + cloudflared
└── src/
    ├── main.go         # HTTP server and VLESS URL generation
    ├── config.go       # Environment variable loading and validation
    ├── wshandler.go    # WebSocket / VLESS protocol handler
    └── go.mod          # Go module definition
```

## Notes

- Keep your `UUID` and `TUNNEL_TOKEN` private — do not commit them to version control.
- TLS is handled by Cloudflare; the Go server only needs to listen on plain HTTP inside the container.
- The container restarts automatically on failure (`restart: unless-stopped` in `docker-compose.yml`).

## License

MIT
