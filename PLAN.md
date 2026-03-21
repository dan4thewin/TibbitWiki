# go-tiddle

A Go replacement for TiddlyWiki's Node.js server, with filesystem
watching and Neovide integration for editing tiddlers from the
terminal or browser.

## Problem

TiddlyWiki's Node.js server has no filesystem watching — edits made
outside the browser (e.g. in a text editor) are invisible until
restart. The codebase is deeply interconnected and hard to extend.
The REST API surface is small (~5 endpoints), making a focused Go
rewrite faster than retrofitting the Node.js code.

## Approach

Reimplement the TiddlyWeb REST API that the TiddlyWiki browser
client already speaks. Add fsnotify-based file watching so external
edits (Neovide, vim, any editor) are picked up automatically. The
browser polls for changes every 3 seconds — no push mechanism
needed for a single-user system.

Serve the TiddlyWiki core JS, plugins, and themes from the existing
`node_modules/tiddlywiki` installation (or a vendored copy).

## Architecture

```
┌─────────────┐     HTTP (3s poll)     ┌──────────────┐
│  TW Browser │ ◄──────────────────► │  go-tiddle   │
│   Client    │                        │   server     │
└─────────────┘                        └──────┬───────┘
                                              │
                              ┌───────────────┼───────────────┐
                              │               │               │
                         fsnotify        .tid files      editor launch
                         watcher      ~/notes/   (neovide)
                                       tiddlers/
```

### REST API (TiddlyWeb compatible)

| Method   | Path                                | Purpose                    |
|----------|-------------------------------------|----------------------------|
| `GET`    | `/`                                 | Serve main wiki HTML       |
| `GET`    | `/status`                           | Server status + user info  |
| `GET`    | `/recipes/default/tiddlers.json`    | Skinny tiddler list (poll) |
| `GET`    | `/recipes/default/tiddlers/:title`  | Full tiddler as JSON       |
| `PUT`    | `/recipes/default/tiddlers/:title`  | Save tiddler               |
| `DELETE` | `/bags/default/tiddlers/:title`     | Delete tiddler             |
| `GET`    | `/favicon.ico`                      | Favicon                    |

### File format

`.tid` files use a simple header/body format:

```
created: 20230710174528965
modified: 20230710232609285
tags: Journal
title: 10th July 2023

Body text goes here, separated from headers by a blank line.
```

The server parses and serializes this format. System tiddlers use
`$__` prefix in filenames (replacing `$:/` from titles). Some
filenames are URL-encoded (containing `%2F` etc.) — this must be
handled.

### File watching

- Use `fsnotify` to watch the tiddlers directory
- Debounce events (cloud drives like OneDrive may write temp files,
  do atomic renames, or fire multiple events per save)
- On change: re-read the affected `.tid` file, update in-memory
  store, bump revision counter
- The browser's 3-second poll picks up the new revision naturally

### Editor integration

**Browser-triggered editing:**
- Add a small TiddlyWiki plugin (a system tiddler) that adds an
  "edit in nvim" button beside the existing pencil (edit) and +
  (new) buttons
- Button sends a request to a go-tiddle endpoint (e.g.
  `POST /edit-external/:title`) which resolves the tiddler's file
  path and launches the configured editor command

**Direct file editing:**
- Just open the `.tid` file in any editor. fsnotify handles the rest.
- No CLI command or API call needed.

**Editor command:** Configurable, default `neovide -- %s`. The `%s`
is replaced with the absolute file path. For a running Neovide
instance, `nvr --servername <addr> --remote %s` could be used
instead.

### Configuration

YAML or TOML config file, e.g. `go-tiddle.yaml`:

```yaml
wiki_path: ~/notes
listen: 127.0.0.1:8080
editor_cmd: "neovide -- %s"
poll_interval: 3s
tw_core_path: ~/node_modules/tiddlywiki  # or vendored
```

Also support command-line flags and environment variables for all
options.

### Serving TiddlyWiki core

The browser needs the full TiddlyWiki application (core JS, boot
code, plugins, themes). Two approaches:

1. **Render at startup**: Use the Node.js TiddlyWiki to render a
   single `index.html` with all core code baked in, then serve that
   statically. The go server just serves this file for `GET /`.
   Simplest approach — run `tiddlywiki --render` once at build/start
   time.

2. **Serve components individually**: Serve boot.js, core plugin,
   theme, etc. as separate requests. More complex, probably
   unnecessary.

**Recommendation**: Option 1. Pre-render the HTML shell with
TiddlyWiki's own tooling, serve it as a static file. The REST API
handles all dynamic tiddler data.

### Plugins to support

From `tiddlywiki.info`:
- `tiddlywiki/tiddlyweb` — sync adaptor (baked into rendered HTML)
- `tiddlywiki/filesystem` — not needed (Go server replaces this)
- `tiddlywiki/highlight` — syntax highlighting plugin

Themes:
- `tiddlywiki/vanilla` + `tiddlywiki/snowwhite`
- Twilight palette (stored as a tiddler, served via API)

## Coding standards

Carried from dan4thewin/hunted-go:

- **Git:** Feature branches. Diff review before commit. Short,
  concise commit messages.
- **Build:** `go build ./...` from repo root. Run `go vet ./...`
  and `gofmt -l .` after every change batch.
- **Test:** `go test -gcflags=all=-l -count=1 ./...` (inlining
  disabled for gomonkey). Statement coverage via
  `go test -gcflags=all=-l -coverprofile=cover.out ./...` then
  `go tool cover -func=cover.out`. Condition coverage via
  `gobco -list-all -test=-gcflags=all=-l .`.
- **Coverage target:** 100% statement + 100% condition coverage.
- **Complexity:** `gocyclo -top N .` — threshold is 2× average CC.
  Flat switches (each case is a single call/assignment) are
  acceptable above threshold.
- **Test style:** Clarity over DRY. Each test self-contained and
  obvious. Copious comments. Descriptive CamelCase subtest names.
- **Mock external deps with gomonkey/v2.**
- **No backup files** in the repo.
- **Surface contradictions immediately** — flag conflicts between
  docs/comments and actual behavior before proceeding.
- **Prefer config over hardcoded constants** where feasible.
- **DRY helpers** with ≤5 params; inline when generics hurt clarity.

## Todo

- [ ] Project scaffolding: go mod init, directory structure,
      .gitignore, Makefile with build/test/lint/coverage targets
- [ ] .github/copilot-instructions.md with coding standards
- [ ] Config: YAML config file parsing with CLI flag overrides
      (wiki_path, listen addr, editor_cmd, poll_interval, tw_core_path)
- [ ] TID parser: read/write .tid file format (headers + body),
      handle URL-encoded filenames, $__ system tiddler naming
- [ ] Tiddler store: in-memory store with revision tracking
      (title → tiddler map, change counter per tiddler)
- [ ] File loader: scan tiddlers directory at startup, parse all
      .tid files into the store
- [ ] File watcher: fsnotify on tiddlers directory, debounce,
      re-read changed files, update store, handle cloud drive quirks
      (temp files, atomic renames, rapid successive events)
- [ ] HTTP server: net/http router with the 7 TiddlyWeb-compatible
      endpoints listed in Architecture section
- [ ] GET / : serve pre-rendered TiddlyWiki HTML
- [ ] GET /status : return JSON with username, anonymous flag,
      read_only, tiddlywiki_version, space recipe
- [ ] GET /recipes/default/tiddlers.json : return skinny tiddler
      list (all fields except text body)
- [ ] GET /recipes/default/tiddlers/:title : return full tiddler JSON
- [ ] PUT /recipes/default/tiddlers/:title : save tiddler to store
      and write .tid file, return ETag with revision
- [ ] DELETE /bags/default/tiddlers/:title : delete from store and
      remove .tid file
- [ ] GET /favicon.ico : serve favicon from $:/favicon.ico tiddler
      or default
- [ ] Editor launch: POST /edit-external/:title endpoint, resolve
      file path, exec configured editor command
- [ ] TW plugin: system tiddler that adds "edit externally" buttons
      beside existing pencil and + buttons in the browser UI
- [ ] Index HTML generation: script/Makefile target to render the
      TiddlyWiki shell HTML using node tiddlywiki, including core,
      tiddlyweb plugin, highlight plugin, snowwhite theme
- [ ] Integration test: start server, load tiddlers from a test
      fixture directory, verify all endpoints return correct data
- [ ] CI setup: GitHub Actions workflow for build, test, vet, gofmt,
      gocyclo, coverage reporting
- [ ] README.md with installation, configuration, and usage docs
