# Go VLESS Tunnel

A Dockerized VLESS proxy server written in Go, integrated with Cloudflare Tunnel for simple and secure deployment.

## Features

- VLESS protocol over WebSocket
- Automatic validation of UUID, port, and domain format
- WebSocket upgrade is accepted only on the configured VLESS path
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

- [Docker](https://docs.docker.com/get-docker/) with the Compose plugin (`docker compose`). Runs on amd64, arm64, arm and 386 hosts; non-amd64 builds need BuildKit, the default builder since Docker 23.
- A Cloudflare account with your domain added to it

## Cloudflare Tunnel Setup

1. **Create a tunnel**
   - In the [Cloudflare dashboard](https://dash.cloudflare.com/), go to **Networking → Tunnels** and select **Create a tunnel**.
   - Enter a name (e.g. `go-vless-tunnel`) and select **Create Tunnel**.
   - Copy the token from the installation command shown (the long string after `--token`). Don't run that command: the container runs `cloudflared` itself, and the tunnel shows as connected once you start it (see Quick Start).

2. **Add a published application route**
   - Open the tunnel and, on the **Routes** tab, select **Add route → Published application**.
   - Enter a subdomain and pick your domain (e.g. `tunnel.yourdomain.com`).
   - Set **Service URL** to `http://localhost:3182` (or whatever `PORT` you configure), then select **Add route**.

3. **Save the values you will need**
   - Tunnel token → `TUNNEL_TOKEN`
   - Hostname → `TUNNEL_DOMAIN`

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

# Optional (defaults shown, except NAME, which defaults to the hostname)
PORT=3182
NAME=my-server
VLESS_PATH_PREFIX=/work-tunnel-
```

### 3. Run with Docker Compose

The repository ships with a ready-to-use `docker-compose.yml` that builds the image from source:

```bash
docker compose up -d
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
| `PORT` | No | `3182` | Port the Go server listens on inside the container |
| `NAME` | No | hostname | Label embedded in the VLESS path |
| `VLESS_PATH_PREFIX` | No | `/work-tunnel-` | WebSocket path prefix used in the VLESS URL |

## Retrieving Your VLESS Link

Once the container is running, open the following URL in a browser or `curl` it:

```
https://<TUNNEL_DOMAIN>/<UUID>
```

The server returns a ready-to-use VLESS connection string that you can paste directly into your proxy client.

The generated URL includes a WebSocket path derived from:

```
<VLESS_PATH_PREFIX><NAME>
```

Example (with defaults):

```
/work-tunnel-my-server
```

Only this path accepts WebSocket upgrade requests.

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
    ├── go.mod          # Go module definition
    └── go.sum          # Module checksum file
```

## Notes

- Keep your `UUID` and `TUNNEL_TOKEN` private — do not commit them to version control.
- TLS is handled by Cloudflare; the Go server only needs to listen on plain HTTP inside the container.
- The container restarts automatically on failure (`restart: unless-stopped` in `docker-compose.yml`).

## License

MIT
