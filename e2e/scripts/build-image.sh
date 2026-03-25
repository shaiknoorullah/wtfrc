#!/usr/bin/env bash
# Build the E2E test VM image from the Arch Linux cloud image.
# Usage: ./build-image.sh [output-dir]
#
# This script:
# 1. Downloads the official Arch Linux cloud image (if not cached)
# 2. Generates an SSH keypair for the test runner
# 3. Creates a cloud-init seed ISO with the user-data
# 4. Boots the VM for provisioning
# 5. Copies tool configs into the guest
# 6. Shuts down and creates a snapshot

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
E2E_DIR="$(dirname "$SCRIPT_DIR")"
OUTPUT_DIR="${1:-${E2E_DIR}/.cache}"

IMAGE_URL="https://geo.mirror.pkgbuild.com/images/latest/Arch-Linux-x86_64-cloudimg.qcow2"
BASE_IMAGE="${OUTPUT_DIR}/arch-base.qcow2"
SNAPSHOT="${OUTPUT_DIR}/arch-e2e.qcow2"
SEED_ISO="${OUTPUT_DIR}/seed.iso"
SSH_KEY="${OUTPUT_DIR}/e2e_key"
SSH_PORT=2222

mkdir -p "$OUTPUT_DIR"

# ---- Step 1: Download base image ----
if [[ ! -f "$BASE_IMAGE" ]]; then
    echo "==> Downloading Arch Linux cloud image..."
    curl -fSL -o "$BASE_IMAGE" "$IMAGE_URL"
else
    echo "==> Base image cached at $BASE_IMAGE"
fi

# ---- Step 2: Generate SSH key ----
if [[ ! -f "$SSH_KEY" ]]; then
    echo "==> Generating SSH keypair..."
    ssh-keygen -t ed25519 -f "$SSH_KEY" -N "" -q
fi

# ---- Step 3: Create cloud-init seed ISO ----
echo "==> Creating cloud-init seed ISO..."
PUBKEY=$(cat "${SSH_KEY}.pub")
USERDATA_TMP="${OUTPUT_DIR}/user-data"

# Replace placeholder SSH key with the real one
sed "s|ssh-ed25519 PLACEHOLDER_KEY_WILL_BE_REPLACED_AT_BUILD_TIME|${PUBKEY}|" \
    "${E2E_DIR}/image/user-data" > "$USERDATA_TMP"

# Create minimal meta-data
cat > "${OUTPUT_DIR}/meta-data" <<EOF
instance-id: wtfrc-e2e
local-hostname: wtfrc-e2e
EOF

# Create seed ISO (cloud-localds or genisoimage)
if command -v cloud-localds &>/dev/null; then
    cloud-localds "$SEED_ISO" "$USERDATA_TMP" "${OUTPUT_DIR}/meta-data"
elif command -v genisoimage &>/dev/null; then
    genisoimage -output "$SEED_ISO" -volid cidata -joliet -rock \
        "$USERDATA_TMP" "${OUTPUT_DIR}/meta-data"
else
    echo "ERROR: cloud-localds or genisoimage required (install cloud-image-utils)"
    exit 1
fi

# ---- Step 4: Create overlay image from base ----
echo "==> Creating qcow2 overlay..."
qemu-img create -f qcow2 -b "$BASE_IMAGE" -F qcow2 "$SNAPSHOT"

# Resize the image to 10G for package installs
qemu-img resize "$SNAPSHOT" 10G

# ---- Step 5: Boot for provisioning ----
echo "==> Booting VM for provisioning (this may take a few minutes)..."

# Detect KVM availability
ACCEL="kvm"
CPU_OPT="-cpu host"
if [[ ! -w /dev/kvm ]] 2>/dev/null; then
    echo "==> /dev/kvm not available, falling back to TCG (slow)"
    ACCEL="tcg"
    CPU_OPT=""
fi

qemu-system-x86_64 \
    -machine "type=q35,accel=${ACCEL}" \
    ${CPU_OPT} \
    -m 2048 \
    -smp 2 \
    -nographic \
    -drive file="$SNAPSHOT",if=virtio,format=qcow2 \
    -drive file="$SEED_ISO",if=virtio,format=raw \
    -device virtio-net-pci,netdev=net0 \
    -netdev user,id=net0,hostfwd=tcp::${SSH_PORT}-:22 \
    -serial mon:stdio \
    </dev/null >"${OUTPUT_DIR}/qemu-provision.log" 2>&1 &

QEMU_PID=$!

# Wait for SSH to become available
echo "==> Waiting for SSH..."
for i in $(seq 1 120); do
    if ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null \
           -o ConnectTimeout=2 -i "$SSH_KEY" -p "$SSH_PORT" \
           test@localhost "echo ready" 2>/dev/null; then
        echo "==> SSH is ready"
        break
    fi
    if [[ $i -eq 120 ]]; then
        echo "ERROR: SSH did not become ready within 120s"
        kill $QEMU_PID 2>/dev/null || true
        exit 1
    fi
    sleep 1
done

CONFIGS_DIR="${E2E_DIR}/image/configs"
SSH_CMD="ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -i $SSH_KEY -p $SSH_PORT test@localhost"
SCP_CMD="scp -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -i $SSH_KEY -P $SSH_PORT"

# Wait for cloud-init to finish (it sets up users, packages, permissions)
echo "==> Waiting for cloud-init to complete..."
for i in $(seq 1 300); do
    if $SSH_CMD "test -f /var/lib/cloud/instance/boot-finished" 2>/dev/null; then
        echo "==> Cloud-init complete"
        break
    fi
    if [[ $i -eq 300 ]]; then
        echo "ERROR: cloud-init did not finish within 300s"
        kill $QEMU_PID 2>/dev/null || true
        exit 1
    fi
    sleep 1
done

# ---- Step 6: Copy tool configs ----
echo "==> Copying tool configs to guest..."

$SSH_CMD "mkdir -p ~/.config/hypr ~/.config/nvim ~/.config/dunst ~/.config/tmux"
$SCP_CMD "${CONFIGS_DIR}/hyprland.conf" "test@localhost:~/.config/hypr/hyprland.conf"
$SCP_CMD "${CONFIGS_DIR}/zshrc" "test@localhost:~/.zshrc"
$SCP_CMD "${CONFIGS_DIR}/init.lua" "test@localhost:~/.config/nvim/init.lua"
$SCP_CMD "${CONFIGS_DIR}/dunstrc" "test@localhost:~/.config/dunst/dunstrc"

# Create empty interceptor config so Hyprland source doesn't error
$SSH_CMD "touch ~/.config/hypr/wtfrc-intercept.conf"

# ---- Step 7: Shut down cleanly ----
echo "==> Shutting down VM..."
$SSH_CMD "sudo poweroff" || true
wait $QEMU_PID 2>/dev/null || true

echo "==> VM image ready at $SNAPSHOT"
echo "==> SSH key at $SSH_KEY"
