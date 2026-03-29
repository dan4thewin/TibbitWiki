#!/bin/bash
# exp-028: Attachment + table + search integration tests.
# Tests features added since exp-026 (revisions):
#   - Markdown table rendering
#   - Note attachment listings
#   - .md attachment rendering via goldmark
#   - Search mapping subdir .md shadows to parent note
#   - attachment_exclude config
#
# API tests run with curl.  Safari DOM tests require:
#   Safari > Settings > Developer > Allow JavaScript from Apple Events
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
assert_not_contains() {
  local label="$1" needle="$2" haystack="$3"
  if echo "$haystack" | grep -qF "$needle"; then
    echo "  ✗ $label"
    echo "    should NOT contain: $needle"
    FAIL=$((FAIL + 1))
  else
    echo "  ✓ $label"
    PASS=$((PASS + 1))
  fi
}
js() {
  local escaped="${1//\"/\\\"}"
  local result
  result=$(osascript -e "tell application \"Safari\" to do JavaScript \"$escaped\" in front document" 2>&1 | perl -pe 's/\.0$//')
  echo "$result"
}
jsv() { js "$1" >/dev/null 2>&1 || true; }

# Check if Safari JS is available.
SAFARI_JS=false
osascript -e 'tell application "Safari" to set URL of front document to "about:blank"' >/dev/null 2>&1
sleep 1
if osascript -e 'tell application "Safari" to do JavaScript "1+1" in front document' >/dev/null 2>&1; then
  SAFARI_JS=true
fi

REPO="$(cd "$(dirname "$0")/../.." && pwd)"
TMPDIR=$(mktemp -d)
WIKI_DIR="$TMPDIR/testwiki"
mkdir -p "$WIKI_DIR"

# --- Test data ---

# Note with a markdown table.
cat > "$WIKI_DIR/TableTest.md" <<'EOF'
title: TableTest
tags: Test
modified: 20260329000000000

## Scores

| Name  | Score |
|-------|-------|
| Alice |    95 |
| Bob   |    87 |
EOF

# Note with attachments in a subdir.
cat > "$WIKI_DIR/AttachNote.md" <<'EOF'
title: AttachNote
tags: Test
modified: 20260329000000000

A note with attachments below.
EOF
mkdir -p "$WIKI_DIR/AttachNote"
echo "fake png" > "$WIKI_DIR/AttachNote/photo.png"
echo "fake pdf" > "$WIKI_DIR/AttachNote/doc.pdf"
cat > "$WIKI_DIR/AttachNote/doc.pdf.md" <<'EOF'
# Extracted PDF Content

This document discusses the Pemberton railway bridge proposal
from the 2024 council meeting minutes.
EOF
cat > "$WIKI_DIR/AttachNote/readme.md" <<'EOF'
# Readme

This is a **bold** statement.

<script>alert('xss')</script>
EOF

# Note with no attachments (control).
cat > "$WIKI_DIR/PlainNote.md" <<'EOF'
title: PlainNote
tags: Test
modified: 20260329000000000

Just a plain note.
EOF

# Config with attachment_exclude to test filtering.
cat > "$TMPDIR/config.yaml" <<EOF
wiki_path: .
listen: 127.0.0.1:8090
attachment_exclude: [".pdf.md"]
cache_size: 100
max_diffs: 10
EOF

echo "Building..."
cd "$REPO"
go build -o "$TMPDIR/tibbitwiki" .

echo "Starting server on :8090..."
"$TMPDIR/tibbitwiki" --config "$TMPDIR/config.yaml" &
SERVER_PID=$!
trap "kill $SERVER_PID 2>/dev/null; rm -rf $TMPDIR" EXIT
sleep 2

BASE="http://127.0.0.1:8090/testwiki"

echo ""
echo "=== API Tests ==="

echo ""
echo "--- 1. Table rendering ---"
NOTE_JSON=$(curl -s "$BASE/notes/TableTest")
HTML=$(echo "$NOTE_JSON" | python3 -c "import json,sys; print(json.load(sys.stdin).get('html',''))")
assert_contains "table tag in rendered HTML" "<table>" "$HTML"
assert_contains "Alice cell in table" "Alice" "$HTML"
assert_contains "thead present" "<thead>" "$HTML"

echo ""
echo "--- 2. Attachment listing ---"
NOTE_JSON=$(curl -s "$BASE/notes/AttachNote")
ATTACH=$(echo "$NOTE_JSON" | python3 -c "import json,sys; print(json.load(sys.stdin).get('attachments',''))")
assert_contains "attachments field present" "[" "$ATTACH"
# doc.pdf.md should be excluded by attachment_exclude config.
assert_not_contains "pdf.md excluded" "doc.pdf.md" "$ATTACH"
assert_contains "photo.png in list" "photo.png" "$ATTACH"
assert_contains "doc.pdf in list" "doc.pdf" "$ATTACH"
assert_contains "readme.md in list" "readme.md" "$ATTACH"

echo ""
echo "--- 3. Plain note has no attachments ---"
NOTE_JSON=$(curl -s "$BASE/notes/PlainNote")
ATTACH=$(echo "$NOTE_JSON" | python3 -c "import json,sys; print(json.load(sys.stdin).get('attachments',''))")
assert "no attachments on PlainNote" "" "$ATTACH"

echo ""
echo "--- 4. Serve .md attachment as rendered HTML ---"
MD_RESP=$(curl -s "$BASE/AttachNote/readme.md")
assert_contains "rendered as HTML" "<h1>" "$MD_RESP"
assert_contains "bold rendered" "<strong>" "$MD_RESP"
assert_contains "themed page shell" "theme.css" "$MD_RESP"
# Raw HTML should be escaped (safe goldmark, no WithUnsafe).
assert_not_contains "script tag stripped" "<script>" "$MD_RESP"

echo ""
echo "--- 5. Serve .md attachment Content-Type ---"
CT=$(curl -s -o /dev/null -D - "$BASE/AttachNote/readme.md" | grep -i 'content-type' | tr -d '\r')
assert_contains "content-type is text/html" "text/html" "$CT"

echo ""
echo "--- 6. Serve non-.md attachment as-is ---"
PNG_RESP=$(curl -s "$BASE/AttachNote/photo.png")
assert "raw file served" "fake png" "$PNG_RESP"

echo ""
echo "--- 7. Search finds shadow .pdf.md content ---"
SEARCH=$(curl -s "$BASE/search?q=Pemberton")
assert_contains "search returns AttachNote" "AttachNote" "$SEARCH"

echo ""
echo "--- 8. Search for content only in shadow ---"
SEARCH=$(curl -s "$BASE/search?q=railway+bridge")
assert_contains "multi-word search hits shadow" "AttachNote" "$SEARCH"

# --- Safari DOM tests ---
if [ "$SAFARI_JS" = true ]; then
  echo ""
  echo "=== Safari DOM Tests ==="
  osascript -e "tell application \"Safari\" to set URL of front document to \"$BASE/\"" >/dev/null 2>&1
  sleep 4

  echo ""
  echo "--- 9. Open AttachNote, verify attachment section ---"
  jsv 'openNote("AttachNote")'
  sleep 2
  HAS_ATTACH=$(js 'document.querySelector(".note[data-title=AttachNote] .note-attachments") ? "yes" : "no"')
  assert "attachment section present" "yes" "$HAS_ATTACH"
  SUMMARY=$(js 'document.querySelector(".note[data-title=AttachNote] .note-attachments summary").textContent')
  assert_contains "summary shows count" "Attachments (3)" "$SUMMARY"

  echo ""
  echo "--- 10. Expand attachments, verify links ---"
  jsv 'document.querySelector(".note[data-title=AttachNote] .note-attachments summary").click()'
  sleep 1
  LINK_COUNT=$(js 'document.querySelectorAll(".note[data-title=AttachNote] .note-attachments a").length')
  assert "3 attachment links" "3" "$LINK_COUNT"
  FIRST_HREF=$(js 'document.querySelector(".note[data-title=AttachNote] .note-attachments a").getAttribute("href")')
  assert_contains "link has wiki prefix" "testwiki/AttachNote/" "$FIRST_HREF"

  echo ""
  echo "--- 11. Open TableTest, verify table renders ---"
  jsv 'openNote("TableTest")'
  sleep 2
  HAS_TABLE=$(js 'document.querySelector(".note[data-title=TableTest] table") ? "yes" : "no"')
  assert "table element present" "yes" "$HAS_TABLE"
  CELLS=$(js 'document.querySelectorAll(".note[data-title=TableTest] td").length')
  assert "4 table cells" "4" "$CELLS"

  echo ""
  echo "--- 12. PlainNote has no attachment section ---"
  jsv 'openNote("PlainNote")'
  sleep 2
  HAS_ATTACH=$(js 'document.querySelector(".note[data-title=PlainNote] .note-attachments") ? "yes" : "no"')
  assert "no attachment section" "no" "$HAS_ATTACH"
else
  echo ""
  echo "(Safari DOM tests skipped — enable Developer > Allow JavaScript from Apple Events)"
fi

echo ""
echo "================================"
echo "Results: $PASS passed, $FAIL failed"
[ "$FAIL" -eq 0 ] && echo "ALL TESTS PASSED" || echo "SOME TESTS FAILED"
exit "$FAIL"
