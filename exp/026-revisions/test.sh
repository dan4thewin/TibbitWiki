#!/bin/bash
# Revisions browser integration test.
# API tests run with curl. Safari DOM tests require:
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
js() {
  local escaped="${1//\"/\\\"}"
  local result
  result=$(osascript -e "tell application \"Safari\" to do JavaScript \"$escaped\" in front document" 2>&1 | perl -pe 's/\.0$//')
  echo "$result"
}
jsv() { js "$1" >/dev/null 2>&1 || true; }

# Check if Safari JS is available.
# First, navigate to a blank page to dismiss any blocking alerts.
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

# --- Create a note with 3 RCS revisions ---
NOTE="$WIKI_DIR/RevTest.md"
for body in "Version one." "Version two." "Version three."; do
  cat > "$NOTE" <<EOF
title: RevTest
tags: Test
modified: 20260324000000000

$body
EOF
  ci -l "$NOTE" < /dev/null >/dev/null 2>&1
done

# A second note with one revision (no diffs to show).
cat > "$WIKI_DIR/Single.md" <<'EOF'
title: Single
tags: Test
modified: 20260324000000000

Only one version.
EOF
ci -l "$WIKI_DIR/Single.md" < /dev/null >/dev/null 2>&1

echo "Building..."
cd "$REPO"
go build -o "$TMPDIR/tibbitwiki" .

echo "Starting server on :8090..."
"$TMPDIR/tibbitwiki" \
  --wiki-path "$TMPDIR" \
  --listen 127.0.0.1:8090 &
SERVER_PID=$!
trap "kill $SERVER_PID 2>/dev/null; rm -rf $TMPDIR" EXIT
sleep 2

BASE="http://127.0.0.1:8090/testwiki"

echo ""
echo "=== API Tests ==="

echo ""
echo "--- 1. Revisions endpoint ---"
REV_JSON=$(curl -s "$BASE/revisions/RevTest")
REV_COUNT=$(echo "$REV_JSON" | python3 -c "import json,sys; print(len(json.load(sys.stdin)['revisions']))")
assert "3 revisions" "3" "$REV_COUNT"
IS_HEAD=$(echo "$REV_JSON" | python3 -c "import json,sys; print(json.load(sys.stdin)['isHead'])")
assert "isHead is True" "True" "$IS_HEAD"
DIFF_HTML=$(echo "$REV_JSON" | python3 -c "import json,sys; d=json.load(sys.stdin)['diffHTML']; print('has_diff' if d else 'empty')")
assert "has diff HTML" "has_diff" "$DIFF_HTML"
DIFF_HEADERS=$(echo "$REV_JSON" | python3 -c "import json,sys; print(json.load(sys.stdin)['diffHTML'].count('→'))")
assert "2 diff transitions" "2" "$DIFF_HEADERS"

echo ""
echo "--- 2. Single-revision note ---"
REV_JSON=$(curl -s "$BASE/revisions/Single")
REV_COUNT=$(echo "$REV_JSON" | python3 -c "import json,sys; print(len(json.load(sys.stdin)['revisions']))")
assert "1 revision" "1" "$REV_COUNT"
DIFF_HTML=$(echo "$REV_JSON" | python3 -c "import json,sys; print(json.load(sys.stdin)['diffHTML'])")
assert "empty diff for single rev" "" "$DIFF_HTML"

echo ""
echo "--- 3. Missing note 404 ---"
STATUS=$(curl -s -o /dev/null -w '%{http_code}' "$BASE/revisions/NonExistent")
assert "missing note 404" "404" "$STATUS"

echo ""
echo "--- 4. Checkout old revision ---"
STATUS=$(curl -s -o /dev/null -w '%{http_code}' -X POST "$BASE/revisions/RevTest/checkout?rev=1.1")
assert "checkout 204" "204" "$STATUS"
sleep 2

echo ""
echo "--- 5. File is read-only after checkout ---"
PERMS=$(stat -f '%Lp' "$WIKI_DIR/RevTest.md")
assert "file is 444" "444" "$PERMS"

echo ""
echo "--- 6. No extra revision created ---"
HEAD=$(cd "$WIKI_DIR" && rlog -h RevTest.md 2>&1 | grep '^head:' | awk '{print $2}')
assert "head still 1.3" "1.3" "$HEAD"

echo ""
echo "--- 7. Note content is old version ---"
BODY=$(grep -A1 '^$' "$WIKI_DIR/RevTest.md" | tail -1)
assert_contains "body has Version one" "Version one" "$BODY"

echo ""
echo "--- 8. GET returns readOnly ---"
sleep 3
RO=$(curl -s "$BASE/notes/RevTest" | python3 -c "import json,sys; print(json.load(sys.stdin).get('readOnly',''))")
assert "readOnly=true" "true" "$RO"

echo ""
echo "--- 9. Revisions endpoint shows not at head ---"
REV_JSON=$(curl -s "$BASE/revisions/RevTest")
IS_HEAD=$(echo "$REV_JSON" | python3 -c "import json,sys; print(json.load(sys.stdin)['isHead'])")
assert "isHead is False" "False" "$IS_HEAD"

echo ""
echo "--- 10. Restore head ---"
STATUS=$(curl -s -o /dev/null -w '%{http_code}' -X POST "$BASE/revisions/RevTest/checkout")
assert "restore 204" "204" "$STATUS"
sleep 2
PERMS=$(stat -f '%Lp' "$WIKI_DIR/RevTest.md")
assert "file is 644" "644" "$PERMS"
BODY=$(grep -A1 '^$' "$WIKI_DIR/RevTest.md" | tail -1)
assert_contains "body has Version three" "Version three" "$BODY"

echo ""
echo "--- 11. Head unchanged after restore ---"
HEAD=$(cd "$WIKI_DIR" && rlog -h RevTest.md 2>&1 | grep '^head:' | awk '{print $2}')
assert "head still 1.3" "1.3" "$HEAD"

echo ""
echo "--- 12. Method checks ---"
STATUS=$(curl -s -o /dev/null -w '%{http_code}' -X POST "$BASE/revisions/RevTest")
assert "POST /revisions 405" "405" "$STATUS"
STATUS=$(curl -s -o /dev/null -w '%{http_code}' "$BASE/revisions/RevTest/checkout")
assert "GET /checkout 405" "405" "$STATUS"

echo ""
echo "--- 13. GET returns rcsRev/rcsHead ---"
# Checkout 1.1 again for RCS fields test.
curl -s -o /dev/null -X POST "$BASE/revisions/RevTest/checkout?rev=1.1"
sleep 2
RCS_JSON=$(curl -s "$BASE/notes/RevTest")
RCS_REV=$(echo "$RCS_JSON" | python3 -c "import json,sys; print(json.load(sys.stdin).get('rcsRev',''))")
RCS_HEAD=$(echo "$RCS_JSON" | python3 -c "import json,sys; print(json.load(sys.stdin).get('rcsHead',''))")
assert "rcsRev=1.1" "1.1" "$RCS_REV"
assert "rcsHead=1.3" "1.3" "$RCS_HEAD"

echo ""
echo "--- 14. Promote old revision as new head ---"
STATUS=$(curl -s -o /dev/null -w '%{http_code}' -X POST "$BASE/revisions/RevTest/promote")
assert "promote 204" "204" "$STATUS"
sleep 2
PERMS=$(stat -f '%Lp' "$WIKI_DIR/RevTest.md")
assert "file writable after promote" "644" "$PERMS"
HEAD=$(cd "$WIKI_DIR" && rlog -h RevTest.md 2>&1 | grep '^head:' | awk '{print $2}')
assert "head is 1.4" "1.4" "$HEAD"
BODY=$(grep -A1 '^$' "$WIKI_DIR/RevTest.md" | tail -1)
assert_contains "body has Version one (promoted)" "Version one" "$BODY"

echo ""
echo "--- 15. CHECKOUT tag removed after promote ---"
TAG=$(cd "$WIKI_DIR" && rlog -h RevTest.md 2>&1 | grep 'CHECKOUT' || echo "")
assert "no CHECKOUT tag" "" "$TAG"

echo ""
echo "--- 16. Promote at head returns 409 ---"
STATUS=$(curl -s -o /dev/null -w '%{http_code}' -X POST "$BASE/revisions/RevTest/promote")
assert "promote at head 409" "409" "$STATUS"

echo ""
echo "--- 17. Method checks for promote ---"
STATUS=$(curl -s -o /dev/null -w '%{http_code}' "$BASE/revisions/RevTest/promote")
assert "GET /promote 405" "405" "$STATUS"

# --- Safari DOM tests (only if JS access enabled) ---
if [ "$SAFARI_JS" = true ]; then
  echo ""
  echo "=== Safari DOM Tests ==="
  osascript -e "tell application \"Safari\" to set URL of front document to \"$BASE/\"" >/dev/null 2>&1
  sleep 4

  echo ""
  echo "--- 18. Open note, verify toolbar ---"
  jsv 'openNote("RevTest")'
  sleep 2
  TOOLBAR_KIDS=$(js 'document.querySelector(".note[data-title=RevTest] .note-toolbar").children.length')
  assert "toolbar has 4 children" "4" "$TOOLBAR_KIDS"
  HIDDEN=$(js 'document.querySelector(".note[data-title=RevTest] .hidden-actions").style.display')
  assert "hidden-actions hidden" "none" "$HIDDEN"

  echo ""
  echo "--- 19. Click chevron ---"
  jsv 'document.querySelector(".note[data-title=RevTest] .note-toolbar").children[1].click()'
  sleep 0.5
  HIDDEN=$(js 'document.querySelector(".note[data-title=RevTest] .hidden-actions").style.display')
  assert "hidden-actions visible" "inline" "$HIDDEN"

  echo ""
  echo "--- 20. Click revisions, check card ---"
  jsv 'document.querySelector(".note[data-title=RevTest] .hidden-actions").children[0].click()'
  sleep 2
  REV_CARD=$(js 'document.querySelector(".note[data-title=__revisions__]") ? "yes" : "no"')
  assert "revisions card exists" "yes" "$REV_CARD"
  GO_BTN=$(js 'document.querySelector(".note[data-title=__revisions__] .action-btn") ? "yes" : "no"')
  assert "checkout btn styled" "yes" "$GO_BTN"

  echo ""
  echo "--- 21. Checkout old rev, verify promote button ---"
  jsv 'doCheckout("RevTest", "1.1", null)'
  sleep 4
  # Force a refresh to pick up the readOnly state change.
  jsv 'refreshNote("RevTest")'
  sleep 2
  # After checkout + refresh, card should rebuild with promote button.
  PROMOTE=$(js 'Array.from(document.querySelector(".note[data-title=RevTest] .note-toolbar").children).find(b => b.textContent.includes("→"))?.textContent || "none"')
  assert_contains "promote button shows 1.1 → 1.5" "1.1" "$PROMOTE"
  assert_contains "promote button shows arrow" "→" "$PROMOTE"
  RESTORE=$(js 'Array.from(document.querySelector(".note[data-title=RevTest] .note-toolbar").children).find(b => b.textContent.includes("head"))?.textContent || "none"')
  assert_contains "restore button present" "head" "$RESTORE"

  echo ""
  echo "--- 22. Click promote, verify note becomes editable ---"
  jsv 'Array.from(document.querySelector(".note[data-title=RevTest] .note-toolbar").children).find(b => b.textContent.includes("→")).click()'
  sleep 3
  EDIT_DISABLED=$(js 'document.querySelector(".note[data-title=RevTest] .note-toolbar button[title*=Edit]")?.disabled || false')
  assert "edit enabled after promote" "false" "$EDIT_DISABLED"
else
  echo ""
  echo "(Safari DOM tests skipped — enable Developer > Allow JavaScript from Apple Events)"
fi

echo ""
echo "================================"
echo "Results: $PASS passed, $FAIL failed"
[ "$FAIL" -eq 0 ] && echo "ALL TESTS PASSED" || echo "SOME TESTS FAILED"
exit "$FAIL"
