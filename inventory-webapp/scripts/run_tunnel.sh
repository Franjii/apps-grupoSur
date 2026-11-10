#!/usr/bin/env bash
# Simple helper to run ngrok or localtunnel for local HTTPS exposure
# Usage:
#   ./scripts/run_tunnel.sh ngrok 3000
#   ./scripts/run_tunnel.sh localtunnel 3000 my-subdomain

set -euo pipefail
provider=${1:-ngrok}
port=${2:-3000}
subdomain=${3:-}

case "$provider" in
  ngrok)
    if ! command -v ngrok >/dev/null 2>&1; then
      echo "ngrok not found. Install from https://ngrok.com/download"
      exit 2
    fi
    # Start ngrok with host-header useful for some local servers
    echo "Starting ngrok http ${port} (press Ctrl+C to stop)"
    ngrok http ${port} --log=stdout
    ;;
  localtunnel|lt)
    if ! command -v npx >/dev/null 2>&1; then
      echo "npx not found. Install Node.js or use npx available with npm"
      exit 2
    fi
    if [ -n "$subdomain" ]; then
      echo "Starting localtunnel on port ${port} with subdomain ${subdomain}"
      npx localtunnel --port ${port} --subdomain ${subdomain}
    else
      echo "Starting localtunnel on port ${port} (subdomain optional)"
      npx localtunnel --port ${port}
    fi
    ;;
  *)
    echo "Unknown provider: $provider"
    echo "Supported: ngrok, localtunnel"
    exit 3
    ;;
esac
