#!/usr/bin/env sh
# Generates .env with fresh secrets for a new Lumen installation.
set -eu

cd "$(dirname "$0")/.."

if [ -f .env ]; then
  echo ".env already exists. Not overwriting it: it holds your vault master key."
  echo "Delete it yourself only if you are starting over with an empty vault."
  exit 1
fi

command -v openssl >/dev/null 2>&1 || { echo "openssl is required"; exit 1; }

umask 077
cat > .env <<EOF
# Lumen configuration. Generated $(date -u +%Y-%m-%dT%H:%M:%SZ)
#
# LUMEN_MASTER_KEY encrypts every file in the vault.
# BACK IT UP somewhere safe and separate from your data.
# If you lose it, your stored evidence cannot be decrypted.
LUMEN_MASTER_KEY=$(openssl rand -base64 32)

# Shared secret between the API and the vault.
VAULT_TOKEN=$(openssl rand -hex 32)

# Shared secret between the API and the metadata extractor.
METADATA_TOKEN=$(openssl rand -hex 32)

POSTGRES_PASSWORD=$(openssl rand -hex 24)

# Port on localhost where Lumen is served.
LUMEN_PORT=8080

# How often stored evidence is re-hashed automatically.
VERIFY_INTERVAL=24h

# Allow capturing pages on private/LAN addresses (default: off).
CAPTURE_ALLOW_PRIVATE=false

# Override the capture browser's User-Agent (default: the browser's own).
CAPTURE_USER_AGENT=

# Files larger than this are not sent to the metadata extractor (bytes, default 1 GiB).
METADATA_MAX_BYTES=1073741824
EOF

echo "Created .env with a new master key."
echo
echo "  IMPORTANT: back up LUMEN_MASTER_KEY from .env now."
echo
echo "Next: docker compose up -d --build   then open http://localhost:8080"
