#!/bin/bash
# exp-test.sh — restart go-tiddle with an experiment's index.html and screenshot
set -eu
exp="$1"
dir="$HOME/src/go-tiddle/exp/$exp"
[[ -f "$dir/index.html" ]] || { echo "No index.html in $dir"; exit 1; }

# Kill existing server
pid=$(lsof -i :8080 -t 2>/dev/null | head -1) && kill "$pid" 2>/dev/null; sleep 1

# Start server
/tmp/go-tiddle \
  --wiki-path ~/notes \
  --index-html "$dir/index.html" \
  --editor-cmd 'neovide -- %s' \
  --listen 127.0.0.1:8080 > /tmp/go-tiddle.log 2>&1 &
sleep 2

# Screenshot
~/src/dpa/scripts/render-url.sh "http://127.0.0.1:8080/?v=$exp" "/tmp/exp-${exp}.png"
