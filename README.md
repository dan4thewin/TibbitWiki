# go-tiddle

A Go replacement for TiddlyWiki's Node.js server with filesystem
watching, external editor integration, and RCS version control.

## Features

- **TiddlyWeb-compatible REST API** — drop-in replacement for the
  Node.js server; the browser client works unchanged
- **Filesystem watching** — edit `.tid` files in any editor (Neovide,
  vim, etc.) and changes appear in the browser within 3 seconds
- **External editor launch** — click a button in the browser to open
  a tiddler in your configured editor
- **RCS version control** — every tiddler save (browser or external)
  is automatically checked into RCS; full revision history per file
- **Simple configuration** — YAML config file with CLI flag overrides

## Quick start

```bash
# Build
go build -o go-tiddle .

# Generate the TiddlyWiki HTML shell (one-time)
tiddlywiki ~/notes --render \
  '$:/plugins/tiddlywiki/tiddlyweb/save/offline' \
  'index.html' 'text/plain'

# Run
./go-tiddle \
  --wiki-path ~/notes \
  --listen 127.0.0.1:8080 \
  --index-html ~/notes/output/index.html \
  --editor-cmd "neovide -- %s"
```

Then open http://127.0.0.1:8080 in your browser.

## Configuration

### CLI flags

| Flag | Default | Description |
|------|---------|-------------|
| `--config` | | Path to YAML config file |
| `--wiki-path` | `.` | Path to TiddlyWiki folder |
| `--listen` | `127.0.0.1:8080` | Host:port to listen on |
| `--editor-cmd` | `neovide -- %s` | Editor command (`%s` = file path) |
| `--index-html` | | Path to pre-rendered index.html |
| `--tw-core-path` | | Path to TiddlyWiki node module |
| `--poll-interval` | `3s` | Browser poll interval (informational) |

### YAML config file

```yaml
wiki_path: ~/notes
listen: 127.0.0.1:8080
editor_cmd: "neovide -- %s"
index_html: ~/notes/output/index.html
```

CLI flags override YAML values.

## API endpoints

| Method | Path | Purpose |
|--------|------|---------|
| GET | `/` | Serve TiddlyWiki HTML |
| GET | `/status` | Server status JSON |
| GET | `/recipes/default/tiddlers.json` | Skinny tiddler list |
| GET | `/recipes/default/tiddlers/:title` | Full tiddler JSON |
| PUT | `/recipes/default/tiddlers/:title` | Save tiddler |
| DELETE | `/bags/default/tiddlers/:title` | Delete tiddler |
| GET | `/favicon.ico` | Favicon |
| POST | `/edit-external/:title` | Launch external editor |

## Editing tiddlers externally

Two ways:

1. **Direct file edit** — open any `.tid` file in your editor, save,
   the server picks up changes via fsnotify.
2. **Browser button** — click the external edit button in the tiddler
   toolbar to launch your configured editor.

## RCS version control

Every tiddler write creates/updates an RCS archive (`filename.tid,v`)
alongside the working file. View history with:

```bash
rlog tiddlers/MyTiddler.tid        # show revision log
rcsdiff tiddlers/MyTiddler.tid     # diff working vs latest
co -r1.2 -p tiddlers/MyTiddler.tid # print revision 1.2
```

Requires `rcs` (`brew install rcs` on macOS).

## Development

```bash
mk build      # go build ./...
mk test       # go test with inlining disabled
mk lint       # go vet + gofmt check
mk coverage   # statement coverage report
mk complexity # gocyclo top 20
mk check      # all of the above
```

Build tool: [mk](https://github.com/dcjones/mk) (Plan 9 mk in Go).
Install: `go install github.com/dcjones/mk@latest`
