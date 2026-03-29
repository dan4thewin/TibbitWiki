title: QuickRef
tags: Reference
created: 2026-03-28T14:57:51Z
modified: 2026-03-29T23:30:00Z

## Note format

A note is a plain `.md` file: RFC 822-style headers, blank line, markdown body.

```
title: MyNote
tags: Project Ideas
created: 2026-03-28T14:57:51Z
modified: 2026-03-28T15:05:13Z

Body text in CommonMark **markdown**.
```

| Header | Required | Description |
|--------|----------|-------------|
| `title` | yes | Display title; must match filename (without `.md`) |
| `tags` | no | Space-separated tag list |
| `created` | auto | ISO 8601 timestamp, set on creation |
| `modified` | auto | ISO 8601 timestamp, updated on save |
| `tab-size` | no | Override code block tab width (e.g. `8` for Go) |

- Header order when saved: `title`, `tags`, then alphabetical.
- `text:` is reserved (holds the body internally).
- Directories with the same base name hold attachments for that note.
- **Renaming:** edit the `title:` header and save. The server renames
  both the `.md` file and its attachment directory to match.

---

## Inline elements

| Syntax | Rendered |
|--------|----------|
| `*Italic*` or `_Italic_` | *Italic* |
| `**Bold**` or `__Bold__` | **Bold** |
| `` `code` `` | `code` |
| `[Link](http://example.com)` | [Link](http://example.com) |
| `![Alt](http://url/img.png)` | (image) |

## Block elements

### Headings

```
# Heading 1
## Heading 2
### Heading 3
```

### Blockquote

```
> Blockquote text
```

> Blockquote text

### Lists

```
- Unordered item         1. Ordered item
- Another item           2. Another item
```

- Unordered item
- Another item

1. Ordered item
2. Another item

### Code block

````
```go
fmt.Println("hello")
```
````

```go
fmt.Println("hello")
```

### Tables

```
| Name | Value |
|------|-------|
| One  | 1     |
| Two  | 2     |
```

| Name | Value |
|------|-------|
| One  | 1     |
| Two  | 2     |

### Horizontal rule

```
---
```

---

## Reference-style links

```
[CommonMark][cmk] is a rationalized spec for Markdown.

[cmk]: https://commonmark.org/
```

[CommonMark][cmk] is a rationalized spec for Markdown.

[cmk]: https://commonmark.org/

## WikiWords

CamelCase words like WikiWord automatically become links to other
notes. Clicking a missing link creates the note.

## Search syntax

| Pattern | Meaning |
|---------|---------|
| `word` | literal match |
| `!word` | exclude notes containing word |
| `#tag` | notes with tag |
| `@title` | match title only |
| `/regex/` | regular expression |
| `a \| b` | either term (OR) |
| `a b` | both terms (AND) |
