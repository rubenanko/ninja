#!/usr/bin/env bash
# Build tinyedit into ./build
set -euo pipefail
cd "$(dirname "$0")"
mkdir -p build
(cd src && go build -o ../build/tinyedit .)
echo "built build/tinyedit"
