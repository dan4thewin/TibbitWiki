# go-tiddle: Start From Scratch Guide

Everything needed to recreate this setup from zero.

## Prerequisites

- macOS (primary machine) with Homebrew
- Go 1.21+ (`brew install go`)
- Node.js (`brew install node`) — needed only to render the TiddlyWiki
  HTML shell
- Neovide (`brew install --cask neovide`)
- Git with SSH key configured for GitHub

## TiddlyWiki setup

### Install TiddlyWiki

```bash
npm install -g tiddlywiki
```

### Create a new wiki

```bash
mkdir -p ~/notes
cd ~/notes
tiddlywiki . --init server
```

This creates `tiddlywiki.info` and a `tiddlers/` directory.

### tiddlywiki.info

The wiki config file should contain:

```json
{
    "description": "Basic client-server edition",
    "plugins": [
        "tiddlywiki/tiddlyweb",
        "tiddlywiki/filesystem",
        "tiddlywiki/highlight"
    ],
    "themes": [
        "tiddlywiki/vanilla",
        "tiddlywiki/snowwhite"
    ],
    "build": {
        "index": [
            "--render",
            "$:/plugins/tiddlywiki/tiddlyweb/save/offline",
            "index.html",
            "text/plain"
        ]
    }
}
```

- `tiddlyweb` — sync adaptor so the browser talks to our server
- `filesystem` — only needed if you ever fall back to the Node.js
  server; go-tiddle replaces it
- `highlight` — syntax highlighting in code blocks
- `snowwhite` — theme (with Twilight palette, stored as a tiddler)

### Browser poll interval

The browser polls the server for changes. Default is 60 seconds,
far too slow for picking up external edits. Create this tiddler
to reduce it to 3 seconds:

**File:** `~/notes/tiddlers/$__config_SyncPollingInterval.tid`

```
title: $:/config/SyncPollingInterval

3000
```

### Palette

The Twilight palette is stored as a tiddler:

**File:** `~/notes/tiddlers/$__palette.tid`

```
created: 20190202142805411
modified: 20230619231744398
title: $:/palette

$:/palettes/Twilight
```

## go-tiddle setup

### Clone and build

```bash
cd ~/src
git clone git@github.com:dan4thewin/go-tiddle.git
cd go-tiddle
go build ./...
```

### Configuration

Config file: `go-tiddle.yaml` (or flags/env vars)

```yaml
wiki_path: ~/notes
listen: 127.0.0.1:8080
editor_cmd: "neovide -- %s"
poll_interval: 3s
tw_core_path: /usr/local/lib/node_modules/tiddlywiki
```

- `wiki_path` — directory containing `tiddlywiki.info` and `tiddlers/`
- `listen` — host:port for the HTTP server
- `editor_cmd` — command to launch when editing externally. `%s`
  is replaced with the absolute path to the `.tid` file
- `poll_interval` — how often the browser polls (informational;
  the actual interval is set in the browser-side tiddler above)
- `tw_core_path` — where TiddlyWiki is installed (for rendering
  the index HTML)

### Generate index.html

The server serves a pre-rendered HTML file containing all TiddlyWiki
core code, plugins, and themes. Generate it:

```bash
# TODO: this could be an mk target
tiddlywiki ~/notes --render \
  '$:/plugins/tiddlywiki/tiddlyweb/save/offline' \
  'index.html' 'text/plain'
```

The output goes to `~/notes/output/index.html`. The go
server serves this file for `GET /`.

### Run the server

```bash
go-tiddle --config go-tiddle.yaml
# or with flags:
go-tiddle --wiki-path ~/notes --listen 127.0.0.1:8080
```

Then open http://127.0.0.1:8080/ in a browser.

## .tid file format

Every tiddler is a `.tid` file with RFC822-style headers, a blank
line, then the body:

```
created: 20230710174528965
modified: 20230710232609285
tags: Journal
title: 10th July 2023

Body text goes here.
Multiple lines are fine.
```

### Filename conventions

| Tiddler title             | Filename                              |
|---------------------------|---------------------------------------|
| `My Tiddler`              | `My Tiddler.tid`                      |
| `$:/config/Something`     | `$__config_Something.tid`             |
| Titles with `/`           | URL-encoded: `%2Fpath%2Fto%2F.tid`    |

- `$:/` in titles becomes `$__` in filenames
- `/` in titles becomes `%2F` in filenames
- Spaces in titles are literal spaces in filenames

### Editing tiddlers externally

Just open the `.tid` file in any editor, edit, save. The go-tiddle
server watches the filesystem and picks up changes. The browser
sees them on its next poll (≤3 seconds).

No special commands or API calls needed.

## Development tools

```bash
# Build
go build ./...

# Lint
go vet ./...
gofmt -l .

# Test (inlining disabled for gomonkey)
go test -gcflags=all=-l -count=1 ./...

# Coverage
go test -gcflags=all=-l -coverprofile=cover.out ./...
go tool cover -func=cover.out

# Condition coverage
gobco -list-all -test=-gcflags=all=-l .

# Cyclomatic complexity
gocyclo -top 10 .

# Install dev tools
go install github.com/fzipp/gocyclo/cmd/gocyclo@latest
go install github.com/zimmski/gobco@latest
```

## Visual smoke test

A Safari screenshot script from `~/src/dpa/scripts/render-url.sh`
can do final visual verification:

```bash
# Capture baseline from current Node.js server
~/src/dpa/scripts/render-url.sh http://127.0.0.1:8080 /tmp/baseline.png

# Kill node server, start go-tiddle, capture
~/src/dpa/scripts/render-url.sh http://127.0.0.1:8080 /tmp/go-tiddle.png
```

The script opens/reloads Safari, waits for load, captures the
window via `screencapture -l`, and restores focus to the previous
app. Compare the two screenshots to verify visual parity.

