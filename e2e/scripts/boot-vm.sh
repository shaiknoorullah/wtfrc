#!/usr/bin/env bash
# Boot the E2E test VM and wait for SSH readiness.
# Usage: ./boot-vm.sh [cache-dir]
#
# Outputs the QEMU PID to stdout. The VM runs in the background.
# SSH is available on localhost:2222.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
E2E_DIR="$(dirname "$SCRIPT_DIR")"
CACHE_DIR="${1:-${E2E_DIR}/.cache}"

SNAPSHOT="${CACHE_DIR}/arch-e2e.qcow2"
SSH_KEY="${CACHE_DIR}/e2e_key"
SSH_PORT=2222
QEMU_PIDFILE="${CACHE_DIR}/qemu.pid"

if [[ ! -f "$SNAPSHOT" ]]; then
    echo "ERROR: VM image not found at $SNAPSHOT. Run 'make e2e-image' first." >&2
    exit 1
fi

if [[ ! -f "$SSH_KEY" ]]; then
    echo "ERROR: SSH key not found at $SSH_KEY. Run 'make e2e-image' first." >&2
    exit 1
fi

# Kill any existing VM
if [[ -f "$QEMU_PIDFILE" ]]; then
    OLD_PID=$(cat "$QEMU_PIDFILE")
    kill "$OLD_PID" 2>/dev/null || true
    rm -f "$QEMU_PIDFILE"
fi

echo "==> Booting E2E VM..." >&2

# Detect KVM availability
ACCEL="kvm"
CPU_OPT="-cpu host"
if [[ ! -w /dev/kvm ]] 2>/dev/null; then
    echo "==> /dev/kvm not available, falling back to TCG (slow)" >&2
    ACCEL="tcg"
    CPU_OPT=""
fi

# Run QEMU in the background (cannot combine -nographic with -daemonize).
# Using -nographic with serial on stdio; redirect to log file and background.
qemu-system-x86_64 \
    -machine "type=q35,accel=${ACCEL}" \
    ${CPU_OPT} \
    -m 2048 \
    -smp 2 \
    -nographic \
    -drive file="$SNAPSHOT",if=virtio,format=qcow2,snapshot=on \
    -device virtio-net-pci,netdev=net0 \
    -netdev user,id=net0,hostfwd=tcp::${SSH_PORT}-:22 \
    -serial mon:stdio \
    </dev/null >"${CACHE_DIR}/qemu-console.log" 2>&1 &

QEMU_PID=$!
echo "$QEMU_PID" > "$QEMU_PIDFILE"

# Verify QEMU actually started
sleep 1
if ! kill -0 "$QEMU_PID" 2>/dev/null; then
    echo "ERROR: QEMU process $QEMU_PID exited immediately. Check ${CACHE_DIR}/qemu-console.log" >&2
    cat "${CACHE_DIR}/qemu-console.log" >&2 || true
    exit 1
fi

# Wait for SSH readiness
echo "==> Waiting for SSH (PID $QEMU_PID)..." >&2
for i in $(seq 1 90); do
    if ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null \
           -o ConnectTimeout=2 -i "$SSH_KEY" -p "$SSH_PORT" \
           test@localhost "true" 2>/dev/null; then
        echo "==> VM ready (SSH on localhost:$SSH_PORT)" >&2
        echo "$QEMU_PID"
        exit 0
    fi
    # Check QEMU is still running
    if ! kill -0 "$QEMU_PID" 2>/dev/null; then
        echo "ERROR: QEMU process died during boot. Console log:" >&2
        tail -50 "${CACHE_DIR}/qemu-console.log" >&2 || true
        exit 1
    fi
    sleep 1
done

echo "ERROR: VM did not become ready within 90s" >&2
kill "$QEMU_PID" 2>/dev/null || true
exit 1
