# TibbitWiki: Start From Scratch Guide

Everything needed to recreate this setup from zero.

## Prerequisites

- macOS with Homebrew
- Go 1.21+ (`brew install go`)
- Neovide (`brew install --cask neovide`)
- Git with SSH key configured for GitHub
- RCS (`brew install rcs`)
- Safari with Developer menu enabled

### Safari settings for testing

Enable the Developer menu:
**Safari → Settings → Advanced → Show features for web developers**

Then enable JavaScript automation:
**Safari → Developer → Allow JavaScript from Apple Events**

This allows `osascript` to drive Safari for automated integration
tests. See [TESTING.md](TESTING.md) for details.

## Wiki directory setup

Create or use an existing tiddlers directory:

```bash
mkdir -p ~/notes/tiddlers
```

Tiddlers are `.tid` files — RFC822-style headers, blank line, body:

```
created: 20230710174528965
modified: 20230710232609285
tags: Journal
title: 10th July 2023

Body text goes here. Uses **markdown** formatting.
```

### Filename conventions

| Tiddler title             | Filename                              |
|---------------------------|---------------------------------------|
| `My Tiddler`              | `My Tiddler.tid`                      |
| `$:/config/Something`     | `$__config_Something.tid`             |
| Titles with `/`           | URL-encoded: `%2Fpath%2Fto%2F.tid`    |

Spaces in titles are literal spaces in filenames.

## TibbitWiki setup

### Clone and build

```bash
cd ~/src
git clone git@github.com:dan4thewin/tibbitwiki.git
cd tibbitwiki
go build -o tibbitwiki .
```

### Configuration

YAML config file (optional) or CLI flags:

```yaml
wiki_path: ~/notes
listen: 127.0.0.1:8080
editor_cmd: "neovide -- %s"
poll_interval: 3s
```

### Run the server

```bash
./tibbitwiki \
  --wiki-path ~/notes \
  --listen 127.0.0.1:8080 \
  --index-html exp/017-tag-pills/index.html \
  --editor-cmd "scripts/neovide-wrapper.sh %s"
```

Open http://127.0.0.1:8080/ in Safari. You'll see the dark-themed
viewer with your tiddlers in the recent sidebar. Click any tiddler
to open it, click "edit" to launch neovide.

### Neovide wrapper (single-instance editing)

The default `neovide -- %s` spawns a new window per edit.
Use `scripts/neovide-wrapper.sh %s` instead to reuse one window:

- First edit: launches Neovide with a fixed Neovim socket
  (`/tmp/nvim-tibbitwiki.sock`)
- Subsequent edits: opens the file in the existing Neovide via
  `nvim --server --remote`
- Override socket path with `NVIM_LISTEN_ADDRESS` env var

### Editing workflow

1. Click "edit" on a tiddler → neovide opens the `.tid` file
   (or reuses the existing window)
2. Edit in neovide, save
3. Browser updates within 3 seconds (poll-based)
4. Click "+ Journal" for a date-titled entry
5. Click "+ New" for a custom-titled tiddler

## RCS version control

Every tiddler write (browser PUT or external file save) creates
an RCS archive in an `RCS/` subdirectory:

```bash
rlog tiddlers/MyTiddler.tid
rcsdiff tiddlers/MyTiddler.tid
co -r1.2 -p tiddlers/MyTiddler.tid
```

Archives live in `RCS/` alongside the tiddlers directory, not inside
it (avoids the file watcher treating them as tiddlers).

## Development tools

```bash
mk build      # go build ./...
mk test       # go test (inlining disabled for gomonkey)
mk lint       # go vet + gofmt
mk coverage   # statement coverage report
mk complexity # gocyclo top 20
mk check      # all of the above
```

Install mk: `go install github.com/dcjones/mk@latest`

See [TESTING.md](TESTING.md) for browser automation techniques.

