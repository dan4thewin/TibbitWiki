#!/bin/bash
# v2 integration test — verifies rendering, highlighting, search, and tag pills.
# Requires: Safari with "Allow JavaScript from Apple Events", tibbitwiki built.
set -u

PASS=0; FAIL=0
assert() {
  local label="$1" expected="$2" actual="$3"
  if [ "$actual" = "$expected" ]; then
    echo "  ✓ $label"
    PASS=$((PASS + 1))
  else
    echo "  ✗ $label"
    echo "    expected: $expected"
    echo "    actual:   $actual"
    FAIL=$((FAIL + 1))
  fi
}
assert_contains() {
  local label="$1" needle="$2" haystack="$3"
  if echo "$haystack" | grep -qF "$needle"; then
    echo "  ✓ $label"
    PASS=$((PASS + 1))
  else
    echo "  ✗ $label"
    echo "    expected to contain: $needle"
    echo "    actual: ${haystack:0:200}"
    FAIL=$((FAIL + 1))
  fi
}
js() {
  # Escape double quotes for AppleScript string embedding.
  local escaped="${1//\"/\\\"}"
  osascript -e "tell application \"Safari\" to do JavaScript \"$escaped\" in front document" 2>&1 | perl -pe 's/\.0$//'
}
# Fire-and-forget for void JS calls (returns undefined → osascript exit 1).
jsv() { js "$1" >/dev/null 2>&1 || true; }

REPO="$(cd "$(dirname "$0")/../.." && pwd)"
TMPDIR=$(mktemp -d)
TIDDLER_DIR="$TMPDIR/tiddlers"
mkdir -p "$TIDDLER_DIR"

# Create test tiddlers.
cat > "$TIDDLER_DIR/TestMarkdown.tid" <<'EOF'
title: TestMarkdown
tags: IntTest Docs
modified: 20260321000000000
type: text/vnd.tiddlywiki

# Heading One

This has **bold** and a code block:

```python
def hello():
    print("world")
```
EOF

cat > "$TIDDLER_DIR/TestSearch.tid" <<'EOF'
title: TestSearch
tags: IntTest
modified: 20260321000001000
type: text/vnd.tiddlywiki

This tiddler mentions gopher and golang.
EOF

cat > "$TIDDLER_DIR/NoMatch.tid" <<'EOF'
title: NoMatch
tags: Other
modified: 20260321000002000
type: text/vnd.tiddlywiki

Nothing interesting here.
EOF

cat > "$TIDDLER_DIR/ToDo.tid" <<'EOF'
title: ToDo
modified: 20260321000003000
type: text/vnd.tiddlywiki

Integration test landing page.
EOF

echo "Building..."
cd "$REPO"
go build -o "$TMPDIR/tibbitwiki" .

echo "Starting server..."
"$TMPDIR/tibbitwiki" \
  --wiki-path "$TMPDIR" \
  --index-html "$REPO/exp/018-v2-integration/index.html" \
  --editor-cmd "$REPO/exp/mock-editor.sh %s" \
  --listen 127.0.0.1:8090 &
SERVER_PID=$!
trap "kill $SERVER_PID 2>/dev/null; rm -rf $TMPDIR" EXIT
sleep 2

echo ""
echo "=== 1. Page load ==="
osascript -e 'tell application "Safari" to set URL of front document to "http://127.0.0.1:8090"' >/dev/null 2>&1
sleep 3
assert "page title" "TibbitWiki" "$(js 'document.title')"

echo ""
echo "=== 2. Markdown rendering ==="
jsv 'openTiddler("TestMarkdown")'
sleep 1
HEADING=$(js 'var e=document.querySelector("[data-title=TestMarkdown] h1"); e?"found":"missing"')
assert "h1 rendered" "found" "$HEADING"
BOLD=$(js 'var e=document.querySelector("[data-title=TestMarkdown] strong"); e?"found":"missing"')
assert "bold rendered" "found" "$BOLD"

echo ""
echo "=== 3. Syntax highlighting ==="
THEME_CSS=$(curl -s http://127.0.0.1:8090/theme.css | head -5)
assert_contains "theme.css serves chroma" "chroma" "$THEME_CSS"
CODE_BLOCK=$(js 'var e=document.querySelector("[data-title=TestMarkdown] pre code"); e?"found":"missing"')
assert "code block rendered" "found" "$CODE_BLOCK"

echo ""
echo "=== 4. Tag pills ==="
PILLS=$(js 'var c=document.querySelector("[data-title=TestMarkdown]"); c&&c.querySelectorAll(".tag-pill").length||0')
assert "2 tag pills" "2" "$PILLS"
PILL_TEXT=$(js 'var c=document.querySelector("[data-title=TestMarkdown]"); var pp=c.querySelectorAll(".tag-pill"); var t=[]; pp.forEach(function(p){t.push(p.textContent)}); t.join(",")')
assert "pill labels" "IntTest,Docs" "$PILL_TEXT"

echo ""
echo "=== 5. Search with match lines ==="
SEARCH=$(curl -s 'http://127.0.0.1:8090/search?q=gopher')
SEARCH_COUNT=$(echo "$SEARCH" | python3 -c "import json,sys; print(len(json.load(sys.stdin)))")
assert "search returns 1 hit" "1" "$SEARCH_COUNT"
SEARCH_TITLE=$(echo "$SEARCH" | python3 -c "import json,sys; print(json.load(sys.stdin)[0]['title'])")
assert "search hit is TestSearch" "TestSearch" "$SEARCH_TITLE"
MATCH_LINES=$(echo "$SEARCH" | python3 -c "import json,sys; print(len(json.load(sys.stdin)[0]['matches']))")
assert "has match lines" "1" "$MATCH_LINES"

echo ""
echo "=== 6. Search UI ==="
jsv 'doSearch("gopher")'
sleep 1
SEARCH_CARD=$(js 'var c=document.querySelector("[data-title=__search__]"); c?c.querySelector("h2").textContent:"none"')
assert "search card title" "Search: gopher" "$SEARCH_CARD"
RESULT_COUNT=$(js 'document.querySelectorAll(".search-result").length')
assert "1 search result in UI" "1" "$RESULT_COUNT"

echo ""
echo "=== 7. Tag pill click → search ==="
# Click IntTest pill on TestMarkdown
jsv 'var old=document.querySelector("[data-title=__search__]"); if(old) old.remove()'
jsv 'var c=document.querySelector("[data-title=TestMarkdown]"); c.querySelector(".tag-pill").click()'
sleep 1
TAG_SEARCH=$(js 'var c=document.querySelector("[data-title=__search__]"); c?c.querySelector("h2").textContent:"none"')
assert "tag search card" "Search: #IntTest" "$TAG_SEARCH"
TAG_RESULTS=$(js 'document.querySelectorAll(".search-result").length')
assert "tag search finds 2" "2" "$TAG_RESULTS"

echo ""
echo "=== 8. River flows down ==="
FIRST=$(js 'document.getElementById("river").firstElementChild.dataset.title')
LAST=$(js 'document.getElementById("river").lastElementChild.dataset.title')
assert "river first child" "ToDo" "$FIRST"
assert "river last child" "__search__" "$LAST"

echo ""
echo "=== 9. NOT search ==="
NOT_SEARCH=$(curl -s 'http://127.0.0.1:8090/search?q=gopher+!golang')
NOT_COUNT=$(echo "$NOT_SEARCH" | python3 -c "import json,sys; print(len(json.load(sys.stdin)))")
assert "gopher !golang = 0 results" "0" "$NOT_COUNT"

echo ""
echo "================================"
echo "Results: $PASS passed, $FAIL failed"
[ "$FAIL" -eq 0 ] && echo "ALL TESTS PASSED ✓" || echo "SOME TESTS FAILED ✗"
exit "$FAIL"
