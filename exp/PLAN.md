# TibbitWiki Browser Client — Experiment Plan

## Goal

Replace TiddlyWiki's complex browser client with a minimal,
server-authoritative viewer. Markdown rendering with CamelCase
WikiWord auto-linking. View-only in browser; editing via neovim.
The server (TibbitWiki) is the single source of truth.

## What we're building

- Static HTML + JS that the Go server serves for GET /
- Polls server every 3s for changes
- Story river: multiple notes open in a scrollable column
- Recent notes list in sidebar
- External edit button (opens neovim via server endpoint)
- New note + Journal buttons
- Simple dark theme
- No TW syncer, no TW plugin system, no TW wikitext parser

## What we reuse from TibbitWiki

- All REST endpoints (GET/PUT notes, note list, edit-external)
- File watcher + RCS versioning
- .tid file format on disk (server already parses it)

## File format

.tid on disk (existing parser). Note body is markdown.
Existing wikitext notes render as-is (raw text) until
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

### 002 — Fetch note list
**Goal:** Fetch the skinny note list from the existing
`GET /notes` endpoint.
- On load, fetch note list, dump titles to the page
- **Pass:** all note titles visible in browser

### 003 — Fetch and display one note
**Goal:** Fetch a single note (ToDo) by title and display
its raw text.
- Fetch `GET /notes/:title`
- Display title as `<h2>`, body as `<pre>`
- **Pass:** ToDo note content visible

### 004 — Markdown rendering
**Goal:** Render note body as markdown instead of raw text.
- Include a small markdown library (marked.js or similar, via CDN
  or inline)
- Render note body through it
- **Pass:** markdown formatting visible (bold, lists, headings)

### 005 — CamelCase WikiWord links
**Goal:** Auto-detect CamelCase words in rendered markdown and
make them clickable links.
- Post-process rendered HTML: regex for CamelCase
- Clicking a WikiWord fetches and opens that note
- **Pass:** click a CamelCase word, see that note

### 006 — Story river
**Goal:** Multiple notes open in a scrollable column.
- Clicking a WikiWord opens that note below/above the current one
  (don't replace it)
- Each note has a close (×) button
- Boot opens ToDo by default
- **Pass:** open 3 notes, close one, remaining two stay

### 007 — Poll loop
**Goal:** Browser polls server every 3s and updates displayed
notes when content changes on disk.
- GET /notes every 3s
- Compare revisions with last known
- Re-fetch any note whose revision changed
- **Pass:** edit a .tid file on disk, browser shows new content
  within ~3s

### 008 — External edit button
**Goal:** Each note has an "edit in neovim" button that
hits the server's edit-external endpoint.
- Button per note in the toolbar
- POST /edit-external/:title on click
- **Pass:** click button, neovide opens with that file

### 009 — New note + Journal buttons
**Goal:** Create new notes from the browser.
- "New" button: prompts for title, PUTs empty note to server
  (server creates .tid file on disk)
- "Journal" button: auto-generates date-based title
  (e.g., "21st March 2026"), PUTs to server, opens in neovim
- **Pass:** click Journal, neovide opens new date-titled file

### 010 — Recent sidebar
**Goal:** Sidebar showing recently modified notes.
- Sorted by `modified` field from note list
- Click to open in story river
- Updates on each poll
- **Pass:** sidebar shows recent notes, clicking opens them

### 011 — Dark theme
**Goal:** Clean, simple dark theme.
- Dark background, light text, styled note cards
- Note toolbar (close, edit-external) styled
- Sidebar styled
- **Pass:** looks good, readable, not ugly

### 012 — Integration test
**Goal:** Full round-trip: create note, edit in neovim,
see changes in browser, navigate via WikiWords.
- Combines all experiments into the final index.html
- Test the complete workflow
- **Pass:** DG uses it for a real editing session without
  reaching for the old TW interface

## Out of scope

- Hugo integration
- Auto-conversion of wikitext to markdown
- Multiple simultaneous browser clients

## Status

v1 complete (experiments 001–012). v2 complete (experiments 013–018).
Final client: `017-tag-pills/index.html`.

## v2 Experiments

### 013 — Server-side markdown rendering
**Goal:** Move markdown rendering from browser (marked.js) to
server (goldmark). Browser receives pre-rendered HTML.
- Add goldmark to GET /notes/:title
- Return `html` field alongside `text` in JSON response
- Client uses `t.html` instead of `marked.parse(t.text)`
- Remove marked.js CDN dependency from index.html
- **Pass:** note renders identically, no CDN request in network tab

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
- `!word` — NOT: exclude notes matching this term
- `#tag` — notes with this tag (`rg '^tags:.*\btag\b'`)
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
- Click result opens note in river
- Titles only (snippets are v3)
- **Pass:** press `/`, type query, see results, click to open

### 017 — Tag pills
**Goal:** Display tags on notes, click to filter.
- Show tag pills below note title in the header
- Click a tag → searches for all notes with that tag
  (reuses search endpoint: `GET /search?tag=tagname`)
- Add `tag` query param to search endpoint (rg for `^tags:.*tagname`)
- Colored pills (hash tag name to hue)
- **Pass:** tags visible, click tag shows related notes

### 018 — v2 integration test
**Goal:** Full round-trip with all v2 features.
- Server-side rendering + syntax highlighting + search + tags
- Same osascript automation pattern as exp-012
- **Pass:** create note with code block, see highlighting;
  search for it; navigate via tag

## Open decisions

- Where to put the final index.html: embed in Go binary as
  `embed.FS`, or keep as separate static file?

## v3 Experiments

### 019 — Sidebar tabs (Recent + Journals)
**Goal:** Replace single Recent list with tabbed sidebar.
- Three tab headers: Recent | Journals | Tags
- Tab 1 (Recent): non-journal tibbits sorted by modified,
  `--sidebar-limit` shown (default 20), ellipsis expands to all
- Tab 2 (Journals): journal tibbits sorted by modified, same
  ellipsis pattern
- Journal detection: has #Journal tag OR title matches date
  format (`/^\d{1,2}(?:st|nd|rd|th) \w+ \d{4}$/`)
- Limits configurable via CSS custom properties:
  `:root { --sidebar-limit: 20; --tag-limit: 5; }`
- Tags tab is a placeholder (built in exp-020)
- **Pass:** tab switching works, Recent excludes journals,
  Journals tab shows only journals, ellipsis expands

### 020 — Tags tab
**Goal:** Third sidebar tab showing tibbits grouped by tag.
- Each tag is a group header with tibbits listed beneath
- Tags sorted by most-recent tibbit in the group
- Show first `--tag-limit` tibbits per tag (default 5)
- Ellipsis per tag expands that tag's full list
- Skip system tags ($:/ prefix)
- **Pass:** tags tab shows grouped tibbits, expand works

### 021 — Rename notes → tibbits
**Goal:** Rename throughout codebase — Go types/vars, API routes,
client JS, docs.
- API: `/notes/` → `/tibbits/` (etc.)
- Go: type names, variable names, comments
- Client JS: all fetch URLs, display text
- Keep backward compat? TBD — single user, probably not needed
- **Pass:** everything works with new names, old routes 404

### 022 — Render line annotation
**Goal:** Add `data-line="N"` attributes to `<li>` elements in
server-rendered HTML.
- Walk goldmark AST in `internal/render/render.go`
- For each ListItem node, inject `data-line` with source line number
- Only annotate list items (not paragraphs, headings, etc.)
- **Pass:** inspect rendered HTML, `<li>` elements have data-line attrs

### 023 — Line edit
**Goal:** Inline editing of list items.
- On hover over `<li data-line>`, show auto-hiding ✏️ icon
- Click: replace `<li>` content with `<input>` prefilled with
  source line text
- Enter: PATCH `/tibbits/:title/line/:n` with new text, re-fetch
  and re-render the whole tibbit
- Escape: cancel, restore original rendered content
- Server: new PATCH endpoint reads .tid file, replaces line N,
  writes back, bumps revision
- **Pass:** edit a list item in-browser, see re-rendered result

### 024 — Insert below
**Goal:** Insert a new list item after the current one.
- On hover, show ➕ icon alongside ✏️
- Click: open empty `<input>` below current `<li>`
- Enter: POST `/tibbits/:title/line/:n/insert` with new text,
  re-fetch and re-render
- Escape: cancel
- **Pass:** add a new list item in-browser, see it rendered

### 025 — v3 integration test
**Goal:** Full automated test of all v3 features.
- Sidebar tab switching + journal detection
- Tags tab grouping + expand
- Line edit round-trip (edit list item, verify re-render)
- Insert-below round-trip
- osascript automation pattern
- **Pass:** all assertions pass
