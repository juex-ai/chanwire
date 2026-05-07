#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(git rev-parse --show-toplevel)"
cd "$REPO_ROOT"

./scripts/build.sh

mkdir -p "$HOME/.local/bin"

cp bin/chanwire "$HOME/.local/bin/chanwire"

echo "installed: $HOME/.local/bin/chanwire"
echo "run 'chanwire version' to verify"

if [[ ":$PATH:" != *":$HOME/.local/bin:"* ]]; then
    echo "note: add \$HOME/.local/bin to your PATH (e.g. export PATH=\"\$HOME/.local/bin:\$PATH\")"
fi
