#!/usr/bin/env bash

set -euo pipefail

cd "$(dirname "$0")"

if [ -d build]; then
	rm -Rf build
fi

mkdir build

echo "built build/ninja"
(cd src && go build -o ../build/tinyedit .)

