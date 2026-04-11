# RAG Integration Design

Semantic similarity search for TibbitWiki notes, powered by
[akin](https://github.com/dan4thewin/akin-vecsearch).

## Design decisions

### Global index (not per-wiki)

One akin index across all wikis. The wiki name is derived from the
file path (`hold/Note.md` → wiki=`hold`). Filtering by wiki is a
path-prefix filter on query results, not a separate index.

Rationale: cross-wiki semantic search is the primary use case. A
single index gives natural cosine-similarity ranking across
everything. Per-wiki indexes would require separate queries with
incomparable scores.

### Opt-in via config

TibbitWiki has zero external service dependencies today. RAG requires
Ollama running locally. It must be opt-in:

```yaml
rag:
  enabled: false
  ollama_url: http://localhost:11434
  model: nomic-embed-text
```

If enabled but Ollama is unreachable at startup, log a warning and
disable RAG gracefully. The server runs fine without it.

### Dedicated indexing goroutine

Embedding via Ollama takes ~100ms per chunk. This must not block
wiki goroutines. Architecture:

- A dedicated indexing goroutine owns the akin index.
- Watchers send file paths to an indexing channel.
- The indexer drains the channel, calls `idx.IndexFile()` for each
  path, and periodically saves the index to disk.
- Wiki goroutines never touch the index directly.

On startup, all existing notes are indexed (initial build). The
index file is persisted to disk so restarts don't require full
re-embedding if no notes changed.

### Model change = full re-index

If the configured model changes, the existing index is invalid
(mixed-model vectors are mathematically meaningless). The indexer
detects the mismatch, deletes the index, and rebuilds from scratch.
See akin's DESIGN.md for details.

## Endpoint

```
GET /rag?q=<text>&n=<count>&note=<title>&wiki=<name>&full=<bool>
```

Parameters (all optional except one of `q` or `note`):
- `q` — natural language query text (embedded via Ollama, then searched)
- `note` — note title; uses that note's existing vector as the query
  (no embedding call needed — just a lookup + cosine search)
- `n` — max results (default 5)
- `wiki` — filter results to this wiki only
- `full` — if true, include full note body in response (for Goose MCP)

Response:
```json
[
  {
    "title": "Note Title",
    "wiki": "hold",
    "score": 0.87,
    "snippet": "...relevant chunk text..."
  }
]
```

With `?full=true`, each result also includes `"body": "full note markdown"`.

## UI consumers

All three use the same endpoint with different query sources.

### 1. Related panel (auto-populates on note open)

When a note is opened, the client sends `GET /rag?note=<title>&n=5`.
Results appear in a collapsible sidebar or footer panel. No user
action required — the panel populates automatically. This is the
primary UI: a serendipity engine showing "here's other stuff about
this topic you might have forgotten."

### 2. Semantic search box

A search input (alongside or toggled from the existing text search)
that sends `GET /rag?q=<user input>&n=10`. Results ranked by
semantic similarity rather than keyword match. Natural language
queries work: "what did I write about retirement planning" finds
notes even if they don't contain those exact words.

### 3. Goose MCP tool

Same endpoint with `?full=true` to get note bodies for LLM context.
A Goose MCP tool calls the endpoint and feeds results into the
conversation. This is the programmatic consumer — the UI consumers
above serve the human directly.

## Module dependency

During development: `replace` directive in go.mod pointing at the
local akin checkout (`~/src/akin`). Before publishing: switch to
`require github.com/dan4thewin/akin-vecsearch`.

## Future: cross-wiki text search

See ARCHITECTURE.md "Future: cross-wiki text search from wiki picker"
for a complementary feature using rg across all wikis from the wiki
picker screen. That feature is independent of RAG but shares the
"directory = implicit wiki tag" principle.
