# Testing TibbitWiki

Tactics for testing the browser client and server integration
without manual interaction.

## Prerequisites

- Safari with **Developer → Allow JavaScript from Apple Events**
  enabled (see [SETUP.md](SETUP.md))
- TibbitWiki built: `go build -o tibbitwiki .`

## Unit tests

```bash
go test -gcflags=all=-l -count=1 ./...
```

The `-gcflags=all=-l` disables inlining, required for gomonkey
patches in watcher and editor tests.

### gomonkey + `-coverprofile` caveat

On darwin/arm64, gomonkey's binary patching of certain functions
causes SIGBUS when run with `-coverprofile`.  The workaround is a
package-level function variable that tests can override without
binary patching:

```go
// production code
var searchFunc = search.Search  // package-level variable

func (w *Wiki) doSearch(query string) { searchFunc(w.dir, query) }

// test code
orig := searchFunc
searchFunc = func(dir, q string) ([]search.Result, error) { ... }
defer func() { searchFunc = orig }()
```

This pattern is used for `search.Search` in `wiki.go` and
`fsnotify.NewWatcher` in `watcher.go`.  If adding new gomonkey
patches that SIGBUS under coverage, convert them the same way.

### Statement coverage

All 11 `internal/` packages are at 100% statement coverage.

```bash
# Quick check (per-package percentages):
go test -gcflags=all=-l -count=1 -coverprofile=cover.out ./...
go tool cover -func=cover.out | grep -v 100
```

The total will show ~96% because `main.go` (wiring only) has no
tests.  If the `grep` returns only the `main.go` and `total` lines,
coverage is intact.

## Browser automation via osascript

Safari's `do JavaScript` AppleScript command lets you read DOM
state, click buttons, and inject JS — all from the command line.
This is the key technique for automated integration testing.

### Reading DOM state

```bash
# Get text content of an element
osascript -e '
tell application "Safari"
    do JavaScript "document.querySelector(\".note h2\")?.textContent" in front document
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
    do JavaScript "document.querySelector(\".note-body\")?.textContent?.includes(\"search term\") ? \"YES\" : \"NO\"" in front document
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
    do JavaScript "createNote(\"TestTitle\", true)" in front document
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

The `screencapture -l` command captures without reloading — use it
for observing live poll updates.

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
4. Poll picks up new revision → browser re-fetches note
5. DOM updated → osascript confirms marker text visible

## Integration test pattern

### Setup

```bash
# Build
go build -o tibbitwiki .

# Start server (use a persistent shell — not background &)
./tibbitwiki \
  --wiki-path ~/notes \
  --editor-cmd "$PWD/exp/mock-editor.sh %s"
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
    set t to do JavaScript "document.querySelector(\".note h2\")?.textContent" in front document
    set s to do JavaScript "document.querySelectorAll(\"#recent-list li\").length" in front document
    return "Note: " & t & " | Sidebar: " & s & " items"
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
    do JavaScript "document.querySelector(\".note-body\")?.textContent?.includes(\"MOCK_EDIT\") ? \"PASS\" : \"FAIL\"" in front document
end tell'

# 4. Clean up test note
rm ~/notes/personal/21st\ March\ 2026.md
```

### Edit existing note

```bash
# Click edit on the first note
osascript -e '
tell application "Safari"
    do JavaScript "document.querySelector(\".note-toolbar button\").click()" in front document
end tell'

# Wait for mock + watcher + poll
sleep 6

# Verify
osascript -e '
tell application "Safari"
    do JavaScript "document.querySelector(\".note-body\")?.textContent?.includes(\"MOCK_EDIT\") ? \"PASS\" : \"FAIL\"" in front document
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
experiment's index.html, and takes a screenshot.

## Gotchas

- **Safari caches error pages.** If the server dies and Safari shows
  "Can't Connect", restarting the server isn't enough — you need to
  navigate Safari to the URL again.

- **Spaces in note titles.** The editor command template is split
  by `strings.Fields` before `%s` substitution, so file paths with
  spaces are handled correctly. But if you ever change this logic,
  test with a title like "21st March 2026".

- **Screenshots reload caveat.** Some screenshot tools reload the
  page, which resets JS state. Use `screencapture -l` directly to
  capture live poll updates without reloading.

- **Poll timing.** After triggering a change, wait at least 5-6
  seconds: ~1s for the watcher to fire, up to 3s for the next poll
  cycle, plus network/render time.

## Adversarial test audit methodology

An audit of all 11 packages using 5 LLM models (50 runs total) found
that 100% statement coverage hides significant test quality gaps: 8
production bugs, 8 security findings, and 270 total findings with 0
false alarms. The full lab notebook is `AdversarialTestAudit` in the
hold wiki.

### What statement coverage misses

- **Hollow tests** — assertions that pass for the wrong reason (e.g.,
  `echo` exits 0 regardless of arguments, so editor tests pass even
  if `%s` substitution is broken)
- **Live pointer returns** — `store.Get()` returns internal `*tid.Note`;
  callers can mutate store state without going through `Put()`
- **Missing cross-method invariants** — `SetFilePath` before `Put`
  creates a ghost entry that panics in `All()`
- **No adversarial inputs** — tests use benign paths like `/tmp/test.md`,
  never flag-like (`-c.md`), percent-encoded (`%2F`), or spaced paths

### Audit prompt template

The prompt that produced zero false alarms across 270 findings:

```
Review the test quality of internal/{package}. For each test, answer:

1. What does the test claim to verify? (one sentence)
2. Could the test still pass if the code under test had a subtle bug?
   Construct a specific, realistic scenario where the test passes but
   the production code is wrong.
3. Are there important behaviors the tests don't cover at all?

For each issue found, provide:
- The specific test function and what it claims to verify
- Why it might not actually verify that claim
- A concrete scenario (not hypothetical) that would expose the gap
- Severity: HIGH (test provably passes for wrong reason or misses a
  crash/data-loss path) vs MEDIUM (weak assertion, missing edge case)
  vs LOW (cosmetic, unlikely)

Focus on: (a) tests that pass for the wrong reason, (b) missing
adversarial/boundary inputs, (c) broken mocks/patches that don't
intercept, (d) cross-cutting interactions the unit tests miss.
```

For packages with shell exec or user input (server, search, rcs,
editor, render), add item 6: security-relevant behaviors.

### Multi-model findings

Every model found unique issues in every package. No single model
subsumes another:

| Model | Character | Best for |
|-------|-----------|----------|
| GPT-5.4 | Fewest findings, highest hit rate | Prioritization, signal-to-noise |
| Opus 4.5 | Cross-cutting, architectural | System-level design issues |
| Sonnet 4 | Most findings, broadest sweep | Initial coverage audit |
| GPT-5.3-codex | Runs commands, traces paths | Evidence-based verification |
| GPT-5.2-codex | Conservative, never wrong | Reliable minimum bar |
