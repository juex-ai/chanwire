#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(git rev-parse --show-toplevel)"
cd "$REPO_ROOT"

mkdir -p bin

VERSION="$(git describe --always --tags --dirty)"
COMMIT="$(git rev-parse --short HEAD)"

( cd server && go build \
    -ldflags "-X main.version=$VERSION -X main.commit=$COMMIT" \
    -o ../bin/chanwire-server \
    ./cmd/chanwire-server )

( cd cli && go build \
    -ldflags "-X main.version=$VERSION -X main.commit=$COMMIT" \
    -o ../bin/chanwire \
    ./cmd/chanwire )

echo "built bin/chanwire-server bin/chanwire (version=$VERSION commit=$COMMIT)"
