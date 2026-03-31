#!/bin/bash
# First-run "kick the tires" integration test.
# Simulates a brand-new user: empty wiki_path, server seeds demo,
# browser opens, creates a note, edits it, views diffs.
#
# Requires Safari > Settings > Developer > Allow JavaScript from Apple Events
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

# Verify Safari JS access.
osascript -e 'tell application "Safari" to set URL of front document to "about:blank"' >/dev/null 2>&1
sleep 1
if ! osascript -e 'tell application "Safari" to do JavaScript "1+1" in front document' >/dev/null 2>&1; then
  echo "Safari JavaScript from Apple Events not enabled — aborting."
  echo "Enable: Safari > Settings > Developer > Allow JavaScript from Apple Events"
  exit 1
fi

REPO="$(cd "$(dirname "$0")/../.." && pwd)"
TMPDIR=$(mktemp -d)
WIKI_PATH="$TMPDIR/wikis"
# Do NOT create wiki_path — the server should seed it.

echo "Building..."
cd "$REPO"
go build -o "$TMPDIR/tibbitwiki" .

echo "Starting server on :8090 with empty wiki_path..."
"$TMPDIR/tibbitwiki" \
  --wiki-path "$WIKI_PATH" \
  --listen 127.0.0.1:8090 > "$TMPDIR/server.log" 2>&1 &
SERVER_PID=$!
trap "kill $SERVER_PID 2>/dev/null; rm -rf $TMPDIR" EXIT
sleep 3

BASE="http://127.0.0.1:8090"

echo ""
echo "=== 1. First-run seeding ==="

# Server should have created demo dir with seed notes.
assert "demo dir exists" "true" "$([ -d "$WIKI_PATH/demo" ] && echo true || echo false)"
assert "TibbitWiki.md seeded" "true" "$([ -f "$WIKI_PATH/demo/TibbitWiki.md" ] && echo true || echo false)"
assert "QuickRef.md seeded" "true" "$([ -f "$WIKI_PATH/demo/QuickRef.md" ] && echo true || echo false)"

echo ""
echo "=== 2. Single-wiki redirect ==="

# GET / should redirect to /demo/
REDIRECT=$(curl -s -o /dev/null -w '%{redirect_url}' "$BASE/")
assert "/ redirects to /demo/" "$BASE/demo/" "$REDIRECT"

echo ""
echo "=== 3. Open in Safari ==="

osascript -e "tell application \"Safari\" to set URL of front document to \"$BASE/\"" >/dev/null 2>&1
sleep 6

# Should have followed redirect to /demo/ and loaded the app.
URL=$(js 'window.location.href')
assert_contains "landed on /demo/" "/demo/" "$URL"

echo ""
echo "--- 3a. Auto-open or sidebar opens TibbitWiki note ---"
# auto_open should fire, but if Safari's fetch timing is off, open manually.
sleep 3
OPEN_NOTE=$(js 'document.querySelector(".note[data-title=TibbitWiki]") ? "yes" : "no"')
if [ "$OPEN_NOTE" = "no" ]; then
  # auto_open may have raced — open via sidebar click (what a user would do).
  jsv 'openNote("TibbitWiki")'
  sleep 3
  OPEN_NOTE=$(js 'document.querySelector(".note[data-title=TibbitWiki]") ? "yes" : "no"')
fi
assert "TibbitWiki note opened" "yes" "$OPEN_NOTE"

echo ""
echo "--- 3b. Sidebar shows seed notes ---"
SIDEBAR=$(js 'document.querySelector("#recent-list").textContent')
assert_contains "sidebar has TibbitWiki" "TibbitWiki" "$SIDEBAR"
assert_contains "sidebar has QuickRef" "QuickRef" "$SIDEBAR"

echo ""
echo "--- 3c. Welcome content rendered ---"
BODY=$(js 'document.querySelector(".note[data-title=TibbitWiki] .note-body").textContent.substring(0,60)')
assert_contains "body has Welcome" "Welcome" "$BODY"

echo ""
echo "=== 4. Create a new note ==="

# Use createNote directly (simulates the New button flow).
jsv "createNote('KickTheTires', false)"
sleep 3
jsv "openNote('KickTheTires')"
sleep 2

NEW_CARD=$(js 'document.querySelector(".note[data-title=KickTheTires]") ? "yes" : "no"')
assert "new note card exists" "yes" "$NEW_CARD"
assert "file created on disk" "true" "$([ -f "$WIKI_PATH/demo/KickTheTires.md" ] && echo true || echo false)"

echo ""
echo "=== 5. Browser textarea edit ==="

# Shift-click the pencil to open textarea editor.
# startTextareaEdit needs the card and note object — call it via JS.
jsv "
(function() {
  var card = document.querySelector('.note[data-title=KickTheTires]');
  var editBtn = card.querySelector('.note-toolbar button[title*=Edit]');
  var ev = new MouseEvent('click', {shiftKey: true, bubbles: true});
  editBtn.dispatchEvent(ev);
})()
"
sleep 2

TEXTAREA=$(js 'document.querySelector(".note[data-title=KickTheTires] .note-edit-area") ? "yes" : "no"')
assert "textarea opened" "yes" "$TEXTAREA"

# The textarea is pre-filled with the note body text only (no headers).
jsv "
(function() {
  var ta = document.querySelector('.note[data-title=KickTheTires] .note-edit-area');
  ta.value = 'First version of the note.';
  ta.dispatchEvent(new Event('input'));
})()
"
sleep 1
# Click the Save button (first button in the edit bar).
jsv "document.querySelector('.note[data-title=KickTheTires] .note-edit-bar button').click()"
sleep 4

# Verify save persisted to disk.
DISK_BODY=$(cat "$WIKI_PATH/demo/KickTheTires.md" | sed -n '/^$/,$ p' | tail -1)
assert_contains "disk has first version" "First version" "$DISK_BODY"

echo ""
echo "--- 5b. Rendered body shows content ---"
RENDERED=$(js 'document.querySelector(".note[data-title=KickTheTires] .note-body")?.textContent || ""')
assert_contains "rendered body shows content" "First version" "$RENDERED"

echo ""
echo "=== 6. Second edit (creates revision) ==="

# Open textarea again.
jsv "
(function() {
  var card = document.querySelector('.note[data-title=KickTheTires]');
  var editBtn = card.querySelector('.note-toolbar button[title*=Edit]');
  var ev = new MouseEvent('click', {shiftKey: true, bubbles: true});
  editBtn.dispatchEvent(ev);
})()
"
sleep 2
jsv "
(function() {
  var ta = document.querySelector('.note[data-title=KickTheTires] .note-edit-area');
  ta.value = 'Second version with changes.';
  ta.dispatchEvent(new Event('input'));
})()
"
sleep 1
jsv "document.querySelector('.note[data-title=KickTheTires] .note-edit-bar button').click()"
sleep 4

DISK_BODY=$(cat "$WIKI_PATH/demo/KickTheTires.md" | sed -n '/^$/,$ p' | tail -1)
assert_contains "disk has second version" "Second version" "$DISK_BODY"

echo ""
echo "=== 7. View revisions and diff ==="

# Open the chevron menu, then click revisions button.
jsv "
(function() {
  var card = document.querySelector('.note[data-title=KickTheTires]');
  var chevron = Array.from(card.querySelectorAll('.note-toolbar button')).find(b => b.title === 'More actions');
  if (chevron) chevron.click();
})()
"
sleep 1
jsv "
(function() {
  var card = document.querySelector('.note[data-title=KickTheTires]');
  var revBtn = card.querySelector('.hidden-actions button[title=Revisions]');
  if (revBtn) revBtn.click();
})()
"
sleep 3

REV_CARD=$(js 'document.querySelector(".note[data-title=__revisions__]") ? "yes" : "no"')
assert "revisions card opened" "yes" "$REV_CARD"

# With 2 edits + initial create, expect 2 diff transitions (3 revisions).
DIFF_COUNT=$(js 'document.querySelectorAll(".note[data-title=__revisions__] h4").length')
assert "2 diff transitions" "2" "$DIFF_COUNT"

# The diff body should contain our edit content.
DIFF_TEXT=$(js '(document.querySelector(".note[data-title=__revisions__] .note-body")?.innerText || "").substring(0,800)')
assert_contains "diff shows first version" "First version" "$DIFF_TEXT"
assert_contains "diff shows second version" "Second version" "$DIFF_TEXT"

echo ""
echo "=== 8. Search ==="

jsv "doSearch('KickTheTires')"
sleep 3

SEARCH_CARD=$(js 'document.querySelector(".note[data-title=__search__]") ? "yes" : "no"')
assert "search results card" "yes" "$SEARCH_CARD"

SEARCH_HIT=$(js 'document.querySelector(".search-result-title")?.textContent || ""')
assert "search found our note" "KickTheTires" "$SEARCH_HIT"

echo ""
echo "================================"
echo "Results: $PASS passed, $FAIL failed"
[ "$FAIL" -eq 0 ] && echo "ALL TESTS PASSED" || echo "SOME TESTS FAILED"
exit "$FAIL"
