tags: Help
title: TibbitWiki

# Welcome to TibbitWiki

A personal wiki served from plain markdown files on disk.

## Getting started

Your wikis live in `~/TibbitWiki/`.  Each subdirectory is a
separate wiki — this demo wiki is one example.

**To create a new wiki,** just make a directory and restart:

```bash
mkdir ~/TibbitWiki/work
# restart the server (⚙ → Restart, or Ctrl-C and relaunch)
```

Your new wiki appears in the wiki picker at `/`.

**To start taking notes:**

- Click **+ New** to create a note
- Click **+ Journal** for a date-titled daily entry
- Click the **pencil icon** to edit (Shift-click for in-browser editing)
- Use the **search bar** (press `/`) for full-text search

## Note format

Notes are `.md` files with RFC 822-style headers:

```
tags: Project Ideas
title: MyGreatIdea

Body text in **markdown**.
```

The filename matches the title: `MyGreatIdea.md`.

**Renaming:** edit the `title:` header and save — the server
renames both the file and its attachment directory automatically.

## WikiWords

CamelCase words like WikiWord automatically become links.
If the target note exists, the link opens it. If not, clicking
creates the note — so WikiWords are the natural way to grow
your wiki.

## Code blocks

Fenced code blocks get syntax highlighting:

```go
func main() {
	fmt.Println("hello TibbitWiki")
}
```

Set `tab-size: 8` in a note's headers to override the default
tab width for languages like Go or Make.

## Tags

Add tags in the `tags:` header (space-separated). Click any
tag pill to search for all notes with that tag.

## Search syntax

| Pattern | Meaning |
|---------|---------|
| `word` | literal match |
| `!word` | exclude |
| `#tag` | notes with tag |
| `@title` | match title only |
| `/regex/` | regular expression |
| `a\|b\|c` | any term (no spaces) |
| `a b` | both terms |

Space separates tokens. Use `\s` in a regex to match a literal
space (e.g. `/hello\sworld/`).

## Customizing

When you're ready to customize, edit `config.yaml` (click
⚙ → Edit config). You can change colors, fonts, layout,
listen address, and editor command. Changes to theme values
apply live on save.

## More info

- QuickRef — markdown syntax reference with rendered examples
- [SETUP.md](https://github.com/dan4thewin/TibbitWiki/blob/main/SETUP.md) —
  full setup guide: prerequisites, config, editor, PDF conversion
- [README.md](https://github.com/dan4thewin/TibbitWiki) —
  features, API endpoints, RCS versioning, architecture
