#!/bin/bash

SUDO_PATH=$(which sudo)
function sudo() {
  if [ "$UID" -eq 0 ]; then
    "$@"
  else
    ${SUDO_PATH} "$@"
  fi
}

set -ex

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd -- "${SCRIPT_DIR}/.." && pwd)"

export DEBIAN_FRONTEND=noninteractive
ARCH="$(dpkg --print-architecture)"
APT_PACKAGES=(
  iputils-ping
  build-essential
  device-tree-compiler
  gperf
  gdb-multiarch
  libnl-3-dev
  libdbus-1-dev
  libelf-dev
  libmpc-dev
  dwarves
  bc
  openssl
  flex
  bison
  libssl-dev
  python3
  python-is-python3
  texinfo
  kmod
  cmake
  wget
  zstd
  python3-venv
  python3-kconfiglib
)

if [ "${ARCH}" = "amd64" ]; then
  APT_PACKAGES+=(g++-multilib gcc-multilib)
else
  echo "Skipping gcc/g++ multilib packages on ${ARCH}."
fi

sudo apt-get update && \
    sudo apt-get install -y --no-install-recommends "${APT_PACKAGES[@]}" && \
    sudo rm -rf /var/lib/apt/lists/*

# Install buildkit
BUILDKIT_VERSION="v0.2.5"
BUILDKIT_TMPDIR="$(mktemp -d)"
pushd "${BUILDKIT_TMPDIR}" > /dev/null

wget https://github.com/jetkvm/rv1106-system/releases/download/${BUILDKIT_VERSION}/buildkit.tar.zst && \
    sudo mkdir -p /opt/jetkvm-native-buildkit && \
    sudo tar --use-compress-program="unzstd --long=31" -xvf buildkit.tar.zst -C /opt/jetkvm-native-buildkit && \
    rm buildkit.tar.zst
popd
rm -rf "${BUILDKIT_TMPDIR}"

# Ensure Go embed has at least one embeddable file in ./static.
# Fresh clones have static/ gitignored and often empty, which breaks:
#   //go:embed all:static
mkdir -p "${REPO_ROOT}/static"

if ! find "${REPO_ROOT}/static" -type f -name '[!._]*' -print -quit | grep -q .; then
  if command -v npm >/dev/null 2>&1 && [ -f "${REPO_ROOT}/ui/package.json" ]; then
    pushd "${REPO_ROOT}/ui" > /dev/null
    npm ci
    npm run build:device
    popd > /dev/null
  fi
fi

# As a final fallback (for partial/offline setup), keep Go builds unblocked.
if ! find "${REPO_ROOT}/static" -type f -name '[!._]*' -print -quit | grep -q .; then
  cat > "${REPO_ROOT}/static/devcontainer-placeholder.txt" <<'EOF'
Devcontainer placeholder asset.
Run `cd ui && npm ci && npm run build:device` to generate full frontend assets.
EOF
fi
