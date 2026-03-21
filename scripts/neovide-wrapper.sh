#!/bin/bash
# neovide-wrapper.sh — reuse an existing Neovide instance if running.
#
# Usage: neovide-wrapper.sh <file>
# Config: tibbitwiki --editor-cmd "scripts/neovide-wrapper.sh %s"
#
# Uses a fixed Neovim listen socket so subsequent calls open files
# in the same Neovide window via --remote.

set -eu

SOCK="${NVIM_LISTEN_ADDRESS:-/tmp/nvim-tibbitwiki.sock}"
FILE="$1"

if [ -S "$SOCK" ] && nvim --server "$SOCK" --remote-expr '1' >/dev/null 2>&1; then
    # Existing Neovide is alive — open file in it
    nvim --server "$SOCK" --remote "$FILE"
else
    # No running instance — clean stale socket and launch fresh
    rm -f "$SOCK"
    neovide -- --listen "$SOCK" "$FILE"
fi
