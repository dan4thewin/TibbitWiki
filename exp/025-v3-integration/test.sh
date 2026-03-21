#!/bin/bash
# v3 integration test — sidebar tabs, tag groups, line edit, insert.
# Requires: Safari with "Allow JavaScript from Apple Events", Go toolchain.
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
assert_gt() {
  local label="$1" threshold="$2" actual="$3"
  if [ "$actual" -gt "$threshold" ] 2>/dev/null; then
    echo "  ✓ $label ($actual > $threshold)"
    PASS=$((PASS + 1))
  else
    echo "  ✗ $label"
    echo "    expected > $threshold, got: $actual"
    FAIL=$((FAIL + 1))
  fi
}
js() {
  local escaped="${1//\"/\\\"}"
  osascript -e "tell application \"Safari\" to do JavaScript \"$escaped\" in front document" 2>&1 | perl -pe 's/\.0$//'
}
jsv() { js "$1" >/dev/null 2>&1 || true; }

REPO="$(cd "$(dirname "$0")/../.." && pwd)"
TMPDIR=$(mktemp -d)
TIDDLER_DIR="$TMPDIR/tiddlers"
mkdir -p "$TIDDLER_DIR"

# --- Create test notes ---

# A regular note with a list (for line-edit testing).
cat > "$TIDDLER_DIR/TestList.md" <<'EOF'
title: TestList
tags: IntTest
modified: 20260321000010000

Shopping list:

- apples
- bananas
- cherries
EOF

# A journal note (date title + Journal tag).
cat > "$TIDDLER_DIR/21st_March_2026.md" <<'EOF'
title: 21st March 2026
tags: Journal
modified: 20260321000020000

Today's journal entry.
EOF

# Another journal (date-format title, no tag).
cat > "$TIDDLER_DIR/20th_March_2026.md" <<'EOF'
title: 20th March 2026
modified: 20260320000000000

Yesterday's entry.
EOF

# Note with tags for tag-tab testing.
cat > "$TIDDLER_DIR/TaggedNote.md" <<'EOF'
title: TaggedNote
tags: IntTest Docs
modified: 20260321000005000

A note with two tags.
EOF

# A second Docs-tagged note.
cat > "$TIDDLER_DIR/DocTwo.md" <<'EOF'
title: DocTwo
tags: Docs
modified: 20260321000003000

Another docs note.
EOF

# Default landing page.
cat > "$TIDDLER_DIR/ToDo.md" <<'EOF'
title: ToDo
modified: 20260321000030000

- task one
- task two
- task three
EOF

echo "Building..."
cd "$REPO"
go build -o "$TMPDIR/tibbitwiki" .

echo "Starting server on :8090..."
"$TMPDIR/tibbitwiki" \
  --wiki-path "$TMPDIR" \
  --index-html "$REPO/exp/025-v3-integration/index.html" \
  --listen 127.0.0.1:8090 &
SERVER_PID=$!
trap "kill $SERVER_PID 2>/dev/null; rm -rf $TMPDIR" EXIT
sleep 2

echo ""
echo "=== 1. Page load + new /notes API ==="
osascript -e 'tell application "Safari" to set URL of front document to "http://127.0.0.1:8090"' >/dev/null 2>&1
sleep 3
assert "page loaded" "TibbitWiki" "$(js 'document.title')"

# Verify new /notes route works (old /recipes/default/tiddlers.json is gone).
NOTE_COUNT=$(curl -s http://127.0.0.1:8090/notes | python3 -c "import json,sys; print(len(json.load(sys.stdin)))")
assert_gt "notes list has items" "0" "$NOTE_COUNT"
OLD_ROUTE=$(curl -s -o /dev/null -w '%{http_code}' http://127.0.0.1:8090/recipes/default/tiddlers.json)
assert "old route returns 404" "404" "$OLD_ROUTE"

echo ""
echo "=== 2. Sidebar tabs exist ==="
TAB_COUNT=$(js 'document.querySelectorAll("#tab-headers .tab").length')
assert "3 tabs" "3" "$TAB_COUNT"
ACTIVE_TAB=$(js 'document.querySelector("#tab-headers .tab.active").dataset.tab')
assert "active tab is recent" "recent" "$ACTIVE_TAB"

echo ""
echo "=== 3. Recent tab excludes journals ==="
RECENT_TITLES=$(js 'var items=[]; document.querySelectorAll("#recent-list li").forEach(function(li){items.push(li.textContent)}); items.join("|")')
assert_contains "recent has TestList" "TestList" "$RECENT_TITLES"
# Date-titled journals should NOT appear in Recent.
JOURNAL_IN_RECENT=$(echo "$RECENT_TITLES" | grep -c "March 2026" || true)
assert "no journals in recent" "0" "$JOURNAL_IN_RECENT"

echo ""
echo "=== 4. Journals tab ==="
jsv 'document.querySelector("[data-tab=journals]").click()'
sleep 0.5
JOURNAL_TITLES=$(js 'var items=[]; document.querySelectorAll("#journal-list li").forEach(function(li){items.push(li.textContent)}); items.join("|")')
assert_contains "journals has 21st March" "21st March 2026" "$JOURNAL_TITLES"
assert_contains "journals has 20th March" "20th March 2026" "$JOURNAL_TITLES"

echo ""
echo "=== 5. Tags tab ==="
jsv 'document.querySelector("[data-tab=tags]").click()'
sleep 0.5
TAG_GROUPS=$(js 'document.querySelectorAll(".tag-group-header").length')
assert_gt "has tag groups" "0" "$TAG_GROUPS"
# Check Docs tag group exists.
DOCS_GROUP=$(js 'var found="no"; document.querySelectorAll(".tag-group-header").forEach(function(h){if(h.textContent.indexOf("Docs")>=0) found="yes"}); found')
assert "Docs tag group exists" "yes" "$DOCS_GROUP"

echo ""
echo "=== 6. Tab switching back to recent ==="
jsv 'document.querySelector("[data-tab=recent]").click()'
sleep 0.3
ACTIVE_TAB2=$(js 'document.querySelector("#tab-headers .tab.active").dataset.tab')
assert "back to recent" "recent" "$ACTIVE_TAB2"

echo ""
echo "=== 7. data-line annotations ==="
jsv 'openNote("TestList")'
sleep 1
LI_COUNT=$(js 'document.querySelectorAll("[data-title=TestList] li[data-line]").length')
assert "3 annotated list items" "3" "$LI_COUNT"
FIRST_LINE=$(js 'document.querySelector("[data-title=TestList] li[data-line]").dataset.line')
# "- apples" is line 7 of the .md body (line 3 of markdown body)
# Actually: body starts after headers, goldmark sees "Shopping list:\n\n- apples\n..."
# "Shopping list:" is body line 1, blank is line 2, "- apples" is line 3
assert "first li data-line" "3" "$FIRST_LINE"

echo ""
echo "=== 8. Line-edit hover icons ==="
ICON_COUNT=$(js 'document.querySelectorAll("[data-title=TestList] .line-icons").length')
assert "3 icon sets" "3" "$ICON_COUNT"

echo ""
echo "=== 9. Line edit round-trip ==="
# Edit line 3 (apples → APPLES) via the API.
EDIT_RESP=$(curl -s -X POST http://127.0.0.1:8090/line-edit/TestList \
  -H 'Content-Type: application/json' \
  -d '{"line":3,"text":"- APPLES"}')
assert_contains "edit response has APPLES" "APPLES" "$EDIT_RESP"
# Verify file on disk changed.
FILE_CONTENT=$(cat "$TIDDLER_DIR/TestList.md")
assert_contains "file has APPLES" "APPLES" "$FILE_CONTENT"

echo ""
echo "=== 10. Insert-below round-trip ==="
INSERT_RESP=$(curl -s -X POST http://127.0.0.1:8090/line-edit/TestList \
  -H 'Content-Type: application/json' \
  -d '{"line":3,"text":"- avocados","insert":true}')
assert_contains "insert response has avocados" "avocados" "$INSERT_RESP"
FILE_CONTENT2=$(cat "$TIDDLER_DIR/TestList.md")
assert_contains "file has avocados" "avocados" "$FILE_CONTENT2"
# Verify order: APPLES then avocados then bananas.
ORDER=$(grep -n 'APPLES\|avocados\|bananas' "$TIDDLER_DIR/TestList.md" | sort -t: -k1 -n | cut -d: -f2 | tr '\n' '|')
assert "correct order" "- APPLES|- avocados|- bananas|" "$ORDER"

echo ""
echo "=== 11. Line edit bad line ==="
BAD_RESP=$(curl -s -o /dev/null -w '%{http_code}' -X POST http://127.0.0.1:8090/line-edit/TestList \
  -H 'Content-Type: application/json' \
  -d '{"line":999,"text":"nope"}')
assert "out of range = 400" "400" "$BAD_RESP"

echo ""
echo "=== 12. Line edit missing note ==="
MISS_RESP=$(curl -s -o /dev/null -w '%{http_code}' -X POST http://127.0.0.1:8090/line-edit/NoSuchNote \
  -H 'Content-Type: application/json' \
  -d '{"line":1,"text":"x"}')
assert "missing note = 404" "404" "$MISS_RESP"

echo ""
echo "=== 13. Search still works (new routes) ==="
SEARCH=$(curl -s 'http://127.0.0.1:8090/search?q=shopping')
SEARCH_COUNT=$(echo "$SEARCH" | python3 -c "import json,sys; print(len(json.load(sys.stdin)))")
assert "search returns 1 hit" "1" "$SEARCH_COUNT"

echo ""
echo "=== 14. Ellipsis expand ==="
# With only 6 notes, sidebar limit 20, no ellipsis should appear.
HAS_ELLIPSIS=$(js 'document.querySelector("#tab-recent .ellipsis-btn") !== null')
assert "no ellipsis (few notes)" "false" "$HAS_ELLIPSIS"

echo ""
echo "================================"
echo "Results: $PASS passed, $FAIL failed"
[ "$FAIL" -eq 0 ] && echo "ALL TESTS PASSED ✓" || echo "SOME TESTS FAILED ✗"
exit "$FAIL"
