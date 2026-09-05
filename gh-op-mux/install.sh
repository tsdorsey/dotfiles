#!/bin/sh
#
# gh-op-mux
#
# Builds the gh client/multiplexer into bin/gh, ahead of Homebrew.

# script/install sources this from the repo root.

if ! command -v go >/dev/null 2>&1; then
  echo "  go is required to build gh-op-mux."
  return 0 2>/dev/null || exit 0
fi

echo "  Building gh-op-mux into bin/gh."
if [ -d gh-op-mux ]; then
  ( cd gh-op-mux && go build -o ../bin/gh . )
else
  go build -o ../bin/gh .
fi

plugins="$HOME/.config/op/plugins.sh"
if [ -f "$plugins" ]; then
  tmp=$(mktemp)
  grep -v 'alias gh="op plugin run -- gh"' "$plugins" > "$tmp" && mv "$tmp" "$plugins"
fi
