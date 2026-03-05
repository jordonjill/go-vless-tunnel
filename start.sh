#!/bin/sh

# Start the Go server
/app/server &

# Start the cloudflared tunnel
exec cloudflared tunnel --no-autoupdate run --token $TUNNEL_TOKEN
