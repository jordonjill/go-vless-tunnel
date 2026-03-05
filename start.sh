#!/bin/sh

set -eu

# Start the Go server and cloudflared, then supervise both processes.
/app/server &
server_pid=$!

cloudflared tunnel --no-autoupdate run --token "${TUNNEL_TOKEN}" &
cloudflared_pid=$!

cleanup() {
	kill "$server_pid" "$cloudflared_pid" 2>/dev/null || true
}

trap cleanup INT TERM

while :; do
	if ! kill -0 "$server_pid" 2>/dev/null; then
		wait "$server_pid" || true
		kill "$cloudflared_pid" 2>/dev/null || true
		wait "$cloudflared_pid" 2>/dev/null || true
		exit 1
	fi

	if ! kill -0 "$cloudflared_pid" 2>/dev/null; then
		wait "$cloudflared_pid"
		status=$?
		kill "$server_pid" 2>/dev/null || true
		wait "$server_pid" 2>/dev/null || true
		exit "$status"
	fi

	sleep 1
done
