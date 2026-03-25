#!/usr/bin/env bash
# Stop the E2E test VM.
# Usage: ./stop-vm.sh [cache-dir]

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
E2E_DIR="$(dirname "$SCRIPT_DIR")"
CACHE_DIR="${1:-${E2E_DIR}/.cache}"

QEMU_PIDFILE="${CACHE_DIR}/qemu.pid"
SSH_KEY="${CACHE_DIR}/e2e_key"
SSH_PORT=2222

if [[ -f "$QEMU_PIDFILE" ]]; then
    PID=$(cat "$QEMU_PIDFILE")

    # Try graceful shutdown via SSH first
    ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null \
        -o ConnectTimeout=2 -i "$SSH_KEY" -p "$SSH_PORT" \
        test@localhost "sudo poweroff" 2>/dev/null || true

    # Wait up to 10s for process to exit
    for i in $(seq 1 10); do
        if ! kill -0 "$PID" 2>/dev/null; then
            break
        fi
        sleep 1
    done

    # Force kill if still running
    kill "$PID" 2>/dev/null || true
    rm -f "$QEMU_PIDFILE"
    echo "==> VM stopped"
else
    echo "==> No running VM found"
fi
