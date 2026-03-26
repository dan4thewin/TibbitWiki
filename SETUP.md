# TibbitWiki: Start From Scratch Guide

Everything needed to recreate this setup from zero.

## Prerequisites

- Go 1.21+ (`brew install go`)
- Neovide (`brew install --cask neovide` on macOS,
  `brew install neovide` on Linux, or https://neovide.dev)
- Git with SSH key configured for GitHub
- RCS (`brew install rcs` or `apt install rcs`)
- ripgrep (`brew install ripgrep` or `apt install ripgrep`)

### Safari settings for testing (macOS only)

Enable the Developer menu:
**Safari → Settings → Advanced → Show features for web developers**

Then enable JavaScript automation:
**Safari → Developer → Allow JavaScript from Apple Events**

This allows `osascript` to drive Safari for automated integration
tests. See [TESTING.md](TESTING.md) for details.

## Wiki directory setup

Create or use an existing notes directory with wiki subdirectories:

```bash
mkdir -p ~/notes/personal
mkdir -p ~/notes/work
```

Notes are `.md` files — RFC822-style headers, blank line, markdown body:

```
created: 2023-07-10T17:45:28Z
modified: 2023-07-10T23:26:09Z
tags: Journal
title: 2023-07-10

Body text goes here. Uses **markdown** formatting.
```

### Filename conventions

| Note title      | Filename                           |
|-----------------|------------------------------------|
| `My Note`       | `My Note.md`                       |
| `path/to/thing` | `path%2Fto%2Fthing.md` (URL-encoded) |

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

A `config.yaml` is auto-created in the wiki path on first run with
commented defaults (colors, fonts, layout, poll interval, etc.).
You can also specify a config file explicitly or use CLI flags:

```yaml
wiki_path: ~/notes
listen: 127.0.0.1:8080
editor_cmd: "neovide -- %s"
poll_interval: 3s
cache_size: 200

theme:
  bg:     "#1a1a2e"
  accent: "#6cacee"
  # ... see config.yaml for all keys
```

### Run the server

```bash
./tibbitwiki --wiki-path ~/notes
```

Open http://127.0.0.1:8080/ in your browser. You'll see the wiki selector
listing your wiki subdirectories. Click one to open the dark-themed
viewer with your notes in the recent sidebar.

### Neovide wrapper (`vide`)

The default `neovide -- %s` spawns a new window per edit.
Use `scripts/vide` instead for single-instance editing with a
persistent headless nvim:

```yaml
editor_cmd: "vide %s"
```

Three-step launch:
1. **Headless nvim** — starts if not running; listens on
   `~/.local/state/vide/nvim.sock`; persists across GUI restarts
2. **Neovide GUI** — connects to the socket; launched on demand
3. **`--remote` open** — sends the file to the running nvim

The script returns immediately. All output goes to
`~/.log/vide/<day-of-week>` (auto-cleaned weekly).

Symlink `scripts/vide` into your PATH (e.g. `~/bin/vide`).

### Server launcher (`tw-start`)

`scripts/tw-start` launches tibbitwiki detached with day-of-week
logging, similar to `vide`:

```bash
tw-start --wiki-path ~/notes
```

- Launches detached with `disown -h`
- Logs to `~/.log/tw-start/<day-of-week>` (auto-cleaned weekly)
- No auto-restart on crash — absence is the signal

### Editing workflow

1. Click "edit" on a note → neovide opens the `.md` file
   (or reuses the existing window)
2. Edit in neovide, save
3. Browser updates within 3 seconds (poll-based)
4. Click "+ Journal" for a date-titled entry
5. Click "+ New" for a custom-titled note
6. Click ⚙ gear icon to edit config.yaml (theme changes apply live),
   or choose "Restart server" from the gear dropdown for other changes

## RCS version control

Every note write (browser PUT or external file save) creates
an RCS archive in an `RCS/` subdirectory:

```bash
rlog notes/MyNote.md
rcsdiff notes/MyNote.md
co -r1.2 -p notes/MyNote.md
```

Archives live in `RCS/` alongside the notes directory, not inside
it (avoids the file watcher treating them as notes).

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

## Migrating from Evernote

### 1. Export from Evernote

Use [evernote-backup](https://github.com/vzhd1701/evernote-backup)
to download your account as ENEX files:

```bash
python3 -m venv /tmp/evernote-env
source /tmp/evernote-env/bin/activate
pip install evernote-backup

# Authenticate via OAuth (opens a browser)
evernote-backup init-db --oauth

# Sync all notes (downloads into a local SQLite DB)
evernote-backup sync

# Export to ENEX, one file per notebook
evernote-backup export ~/evernote-export/enex/
```

This produces one `.enex` file per notebook in the export directory.

### 2. Convert ENEX to .md notes

`scripts/enex2tid` converts ENEX files to TibbitWiki `.md` format:

```bash
source /tmp/evernote-env/bin/activate   # needs markdownify
pip install markdownify
scripts/enex2tid ~/evernote-export/enex/ ~/notes/evernote/
```

The converter:
- Parses ENML (Evernote's HTML variant) to markdown via markdownify
- Extracts attachments to per-note subdirectories
  (e.g., `MyNote/photo.jpg` alongside `MyNote.md`)
- Converts Evernote timestamps to ISO 8601
- Preserves Evernote tags and notebook name in the `tags:` header
- Strips web-clip artifacts (inline SVGs, data URIs)
- Detects clipped web pages and adds a "Clipped from" source link
- Handles duplicate titles by appending the creation date
- Truncates or hashes overlong attachment filenames
- URL-encodes spaces in attachment links for goldmark compatibility

Output is one `.md` file per note, ready to serve as a wiki directory.

### 3. (Optional) Rename with meaningful WikiWord titles

Evernote note titles are often long URLs, cryptic abbreviations,
or raw filenames. `scripts/wikiname` uses a local LLM (via
[Ollama](https://ollama.com)) to generate terse CamelCase WikiWord
names and coarse classification tags.

This is a separate batch step — it reads a TSV of titles and body
excerpts, queries the model once per note, and writes a mapping
TSV. A post-processing step guarantees CamelCase format compliance
regardless of model output quality. See the script for usage.

