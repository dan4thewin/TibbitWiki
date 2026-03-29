tags: Help
title: TibbitWiki

# Welcome to TibbitWiki

A personal wiki served from plain markdown files on disk.

## Quick start

- Click **+ New** to create a note
- Click **+ Journal** for a date-titled daily entry
- Click the **pencil icon** on any note to edit in Neovide
- Save in your editor — the browser updates within 3 seconds
- Use the **search bar** (press `/`) for full-text search

## Note format

Notes are `.md` files with RFC 822-style headers:

```
tags: Project Ideas
title: MyGreatIdea

Body text in **markdown**.
```

The filename matches the title: `MyGreatIdea.md`.

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
| `a \| b` | either term |
| `a b` | both terms |

## Theme

Edit `config.yaml` (click ⚙ → Edit config) to change colors,
fonts, and layout. Changes apply live on save.

To preview themes visually, symlink the playground into your wiki
directory and browse to `/{wiki}/playground/index.html`:

```bash
ln -s ~/src/tibbitwiki/playground ~/notes/personal/playground
```

The playground reads your current theme and lets you tweak every
variable with instant feedback, then copy a ready-to-paste YAML
block.

## More info

- QuickRef — markdown syntax reference with rendered examples
- [SETUP.md](https://github.com/dan4thewin/TibbitWiki/blob/main/SETUP.md) —
  full setup guide: prerequisites, config, editor, PDF conversion,
  Evernote migration
- [README.md](https://github.com/dan4thewin/TibbitWiki) —
  features, API endpoints, RCS versioning, architecture
