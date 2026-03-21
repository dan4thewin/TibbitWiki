# Testing TibbitWiki

Tactics for testing the browser client and server integration
without manual interaction.

## Prerequisites

- Safari with **Developer → Allow JavaScript from Apple Events**
  enabled (see [SETUP.md](SETUP.md))
- TibbitWiki built: `go build -o /tmp/TibbitWiki .`

## Unit tests

```bash
go test -gcflags=all=-l -count=1 ./...
```

The `-gcflags=all=-l` disables inlining, required for gomonkey
patches in watcher and editor tests.

## Browser automation via osascript

Safari's `do JavaScript` AppleScript command lets you read DOM
state, click buttons, and inject JS — all from the command line.
This is the key technique for automated integration testing.

### Reading DOM state

```bash
# Get text content of an element
osascript -e '
tell application "Safari"
    do JavaScript "document.querySelector(\".tiddler h2\")?.textContent" in front document
end tell'

# Count elements
osascript -e '
tell application "Safari"
    do JavaScript "document.querySelectorAll(\"#recent-list li\").length" in front document
end tell'

# Check computed style
osascript -e '
tell application "Safari"
    do JavaScript "getComputedStyle(document.body).backgroundColor" in front document
end tell'

# Check if text is present
osascript -e '
tell application "Safari"
    do JavaScript "document.querySelector(\".tiddler-body\")?.textContent?.includes(\"search term\") ? \"YES\" : \"NO\"" in front document
end tell'
```

### Clicking elements

```bash
# Click by selector
osascript -e '
tell application "Safari"
    do JavaScript "document.querySelector(\"button\").click()" in front document
end tell'

# Click nth item in a list
osascript -e '
tell application "Safari"
    do JavaScript "document.querySelectorAll(\"#recent-list li\")[2].click()" in front document
end tell'

# Call a JS function directly
osascript -e '
tell application "Safari"
    do JavaScript "createTiddler(\"TestTitle\", true)" in front document
end tell'
```

### Navigation

```bash
# Navigate to a URL (reloads page)
osascript -e '
tell application "Safari"
    set URL of front document to "http://127.0.0.1:8080/"
    activate
end tell'

# Open a new document (if current is an error page)
osascript -e '
tell application "Safari"
    make new document with properties {URL:"http://127.0.0.1:8080/"}
    activate
end tell'
```

### Screenshots

```bash
# Capture Safari window without reloading
WIN_ID=$(osascript -e 'tell application "Safari" to id of front window')
screencapture -l "$WIN_ID" /tmp/screenshot.png
```

The `render-url.sh` script (in `~/src/dpa/scripts/`) also captures
screenshots but reloads the page first — useful for fresh-state
captures, not for observing live poll updates.

## Mock editor

`exp/mock-editor.sh` is a stand-in for neovide that completes
instantly and writes a verifiable marker:

```bash
#!/bin/bash
echo "MOCK_EDIT_$(date +%s)" >> "$1"
```

Use it with `--editor-cmd` to test the edit round-trip:

```bash
./TibbitWiki --editor-cmd "$PWD/exp/mock-editor.sh %s" ...
```

The marker lets you verify the full chain:
1. Browser click → POST /edit-external
2. Server runs mock editor → appends marker to .tid file
3. Watcher detects change → bumps revision
4. Poll picks up new revision → browser re-fetches tiddler
5. DOM updated → osascript confirms marker text visible

## Integration test pattern

### Setup

```bash
# Build
go build -o /tmp/TibbitWiki .

# Start server (use a persistent shell — not background &)
/tmp/TibbitWiki \
  --wiki-path ~/notes \
  --index-html exp/012-integration/index.html \
  --editor-cmd "$PWD/exp/mock-editor.sh %s" \
  --listen 127.0.0.1:8080
```

**Important:** When running from a copilot session, start the server
with `bash mode="async"` and a named shellId. Background processes
started with `&` in sync bash get killed when the shell session
ends, causing the server to die between tool calls.

### Load and verify

```bash
# Navigate Safari
osascript -e '
tell application "Safari"
    set URL of front document to "http://127.0.0.1:8080/"
    activate
end tell'
sleep 3

# Verify initial state
osascript -e '
tell application "Safari"
    set t to do JavaScript "document.querySelector(\".tiddler h2\")?.textContent" in front document
    set s to do JavaScript "document.querySelectorAll(\"#recent-list li\").length" in front document
    return "Tiddler: " & t & " | Sidebar: " & s & " items"
end tell'
```

### Full round-trip test

```bash
# 1. Click Journal button
osascript -e '
tell application "Safari"
    do JavaScript "document.querySelectorAll(\"#top-bar button\")[1].click()" in front document
end tell'

# 2. Wait for mock editor + watcher + poll cycle
sleep 6

# 3. Verify mock edit marker appears in DOM
osascript -e '
tell application "Safari"
    do JavaScript "document.querySelector(\".tiddler-body\")?.textContent?.includes(\"MOCK_EDIT\") ? \"PASS\" : \"FAIL\"" in front document
end tell'

# 4. Clean up test tiddler
rm ~/notes/tiddlers/21st\ March\ 2026.tid
```

### Edit existing tiddler

```bash
# Click edit on the first tiddler
osascript -e '
tell application "Safari"
    do JavaScript "document.querySelector(\".tiddler-toolbar button\").click()" in front document
end tell'

# Wait for mock + watcher + poll
sleep 6

# Verify
osascript -e '
tell application "Safari"
    do JavaScript "document.querySelector(\".tiddler-body\")?.textContent?.includes(\"MOCK_EDIT\") ? \"PASS\" : \"FAIL\"" in front document
end tell'
```

## Experiment test helper

`exp/test.sh` provides a restart-and-screenshot cycle for quickly
testing an experiment's index.html:

```bash
cd ~/src/TibbitWiki/exp
./test.sh 012-integration
```

It kills any existing server, starts a fresh one with the
experiment's index.html, and takes a screenshot via render-url.sh.

## Gotchas

- **Safari caches error pages.** If the server dies and Safari shows
  "Can't Connect", restarting the server isn't enough — you need to
  navigate Safari to the URL again.

- **Spaces in tiddler titles.** The editor command template is split
  by `strings.Fields` before `%s` substitution, so file paths with
  spaces are handled correctly. But if you ever change this logic,
  test with a title like "21st March 2026".

- **render-url.sh reloads the page.** This means you can't use it
  to capture live poll updates (the reload resets JS state). Use
  `screencapture -l` directly for that.

- **Poll timing.** After triggering a change, wait at least 5-6
  seconds: ~1s for the watcher to fire, up to 3s for the next poll
  cycle, plus network/render time.
