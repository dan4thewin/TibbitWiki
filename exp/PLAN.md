# TibbitWiki Browser Client — Experiment Plan

## Goal

Replace TiddlyWiki's complex browser client with a minimal,
server-authoritative viewer. Markdown rendering with CamelCase
WikiWord auto-linking. View-only in browser; editing via neovim.
The server (TibbitWiki) is the single source of truth.

## What we're building

- Static HTML + JS that the Go server serves for GET /
- Polls server every 3s for changes
- Story river: multiple tiddlers open in a scrollable column
- Recent tiddlers list in sidebar
- External edit button (opens neovim via server endpoint)
- New tiddler + Journal buttons
- Simple dark theme
- No TW syncer, no TW plugin system, no TW wikitext parser

## What we reuse from TibbitWiki

- All REST endpoints (GET/PUT tiddlers, tiddler list, edit-external)
- File watcher + RCS versioning
- .tid file format on disk (server already parses it)

## File format

.tid on disk (existing parser). Tiddler body is markdown.
Existing wikitext tiddlers render as-is (raw text) until
manually converted — no auto-conversion.

## Experiments

Each experiment is a self-contained directory under
`~/src/TibbitWiki/exp/` with:
- `index.html` — the experiment's HTML file
- `README.md` — what it proves, how to test
- A test procedure (manual or scripted via render-url.sh)

The TibbitWiki server serves `exp/NNN/index.html` for testing.
Each experiment builds on the last by copying and extending.

### 001 — Bare boot
**Goal:** Serve a minimal HTML page from TibbitWiki. Prove the
static file serving path works.
- Hardcoded HTML: `<h1>TibbitWiki</h1>`
- Server serves it for GET /
- **Pass:** page renders in Safari

### 002 — Fetch tiddler list
**Goal:** Fetch the skinny tiddler list from the existing
`GET /recipes/default/tiddlers.json` endpoint.
- On load, fetch tiddler list, dump titles to the page
- **Pass:** all tiddler titles visible in browser

### 003 — Fetch and display one tiddler
**Goal:** Fetch a single tiddler (ToDo) by title and display
its raw text.
- Fetch `GET /recipes/default/tiddlers/:title`
- Display title as `<h2>`, body as `<pre>`
- **Pass:** ToDo tiddler content visible

### 004 — Markdown rendering
**Goal:** Render tiddler body as markdown instead of raw text.
- Include a small markdown library (marked.js or similar, via CDN
  or inline)
- Render tiddler body through it
- **Pass:** markdown formatting visible (bold, lists, headings)

### 005 — CamelCase WikiWord links
**Goal:** Auto-detect CamelCase words in rendered markdown and
make them clickable links.
- Post-process rendered HTML: regex for CamelCase
- Clicking a WikiWord fetches and opens that tiddler
- **Pass:** click a CamelCase word, see that tiddler

### 006 — Story river
**Goal:** Multiple tiddlers open in a scrollable column.
- Clicking a WikiWord opens that tiddler below/above the current one
  (don't replace it)
- Each tiddler has a close (×) button
- Boot opens ToDo by default
- **Pass:** open 3 tiddlers, close one, remaining two stay

### 007 — Poll loop
**Goal:** Browser polls server every 3s and updates displayed
tiddlers when content changes on disk.
- GET /recipes/default/tiddlers.json every 3s
- Compare revisions with last known
- Re-fetch any tiddler whose revision changed
- **Pass:** edit a .tid file on disk, browser shows new content
  within ~3s

### 008 — External edit button
**Goal:** Each tiddler has an "edit in neovim" button that
hits the server's edit-external endpoint.
- Button per tiddler in the toolbar
- POST /edit-external/:title on click
- **Pass:** click button, neovide opens with that file

### 009 — New tiddler + Journal buttons
**Goal:** Create new tiddlers from the browser.
- "New" button: prompts for title, PUTs empty tiddler to server
  (server creates .tid file on disk)
- "Journal" button: auto-generates date-based title
  (e.g., "21st March 2026"), PUTs to server, opens in neovim
- **Pass:** click Journal, neovide opens new date-titled file

### 010 — Recent sidebar
**Goal:** Sidebar showing recently modified tiddlers.
- Sorted by `modified` field from tiddler list
- Click to open in story river
- Updates on each poll
- **Pass:** sidebar shows recent tiddlers, clicking opens them

### 011 — Dark theme
**Goal:** Clean, simple dark theme.
- Dark background, light text, styled tiddler cards
- Tiddler toolbar (close, edit-external) styled
- Sidebar styled
- **Pass:** looks good, readable, not ugly

### 012 — Integration test
**Goal:** Full round-trip: create tiddler, edit in neovim,
see changes in browser, navigate via WikiWords.
- Combines all experiments into the final index.html
- Test the complete workflow
- **Pass:** DG uses it for a real editing session without
  reaching for the old TW interface

## Out of scope (v2)

- Browser-based editing (textarea)
- Search
- Tag navigation / tag pills
- Hugo integration
- Auto-conversion of wikitext to markdown
- Multiple simultaneous browser clients

## Status

v1 complete (experiments 001–012). Final client: `012-integration/index.html`.

## v2 Experiments

### 013 — Server-side markdown rendering
**Goal:** Move markdown rendering from browser (marked.js) to
server (goldmark). Browser receives pre-rendered HTML.
- Add goldmark to GET /recipes/default/tiddlers/:title
- Return `html` field alongside `text` in JSON response
- Client uses `t.html` instead of `marked.parse(t.text)`
- Remove marked.js CDN dependency from index.html
- **Pass:** tiddler renders identically, no CDN request in network tab

### 014 — Syntax highlighting
**Goal:** Fenced code blocks get syntax highlighting via chroma.
- Add chroma as goldmark extension (goldmark-highlighting)
- Use OneDark theme (or nearest chroma equivalent)
- Generate theme CSS, serve as `/theme.css`
- Test with bash and go code blocks
- **Pass:** code blocks have colored syntax in browser

### 015 — Search endpoint
**Goal:** `GET /search?q=...` wraps ripgrep to search .tid files.

Query syntax:
- `word` — literal substring, case-insensitive (body + title)
- `!word` — NOT: exclude tiddlers matching this term
- `#tag` — tiddlers with this tag (`rg '^tags:.*\btag\b'`)
- `@title` — title substring (`rg '^title:.*title'`)
- `/regex/` — raw regex passed to rg
- `mXregexX` — raw regex, X is any non-alnum delimiter
  (perl-style, e.g. `m;exit|status;` or `m|foo/bar|`)
- `word1 word2` — implicit AND (intersect result sets)
- `word1|word2` — OR (single rg call with `-e`)

Implementation: parse tokens, classify (literal/regex/tag/title/not),
run rg calls, do set intersection/difference in Go, map filenames
back to titles. ~40 lines.

Examples:
```
/search?q=systemd debugging      → AND two literals
/search?q=#linux @journal        → tagged "linux" AND title has "journal"
/search?q=!#old systemd          → has "systemd", NOT tagged "old"
/search?q=/^created: 20260321/   → regex: created today
```

- **Pass:** `curl /search?q=#linux` returns matching titles

### 016 — Search UI
**Goal:** Keyboard-driven search in the browser.
- `/` focuses a search input at the bottom of the viewport
  (vi-style command line)
- Escape clears search and dismisses the input
- Debounced fetch to `/search?q=...` as you type
- Results appear in the sidebar, replacing the recent list
- Click result opens tiddler in river
- Titles only (snippets are v3)
- **Pass:** press `/`, type query, see results, click to open

### 017 — Tag pills
**Goal:** Display tags on tiddlers, click to filter.
- Show tag pills below tiddler title in the header
- Click a tag → searches for all tiddlers with that tag
  (reuses search endpoint: `GET /search?tag=tagname`)
- Add `tag` query param to search endpoint (rg for `^tags:.*tagname`)
- Colored pills (hash tag name to hue)
- **Pass:** tags visible, click tag shows related tiddlers

### 018 — v2 integration test
**Goal:** Full round-trip with all v2 features.
- Server-side rendering + syntax highlighting + search + tags
- Same osascript automation pattern as exp-012
- **Pass:** create tiddler with code block, see highlighting;
  search for it; navigate via tag

## Open decisions

- Where to put the final index.html: embed in Go binary as
  `embed.FS`, or keep as separate static file?
- chroma theme: OneDark if available, else monokai/dracula
