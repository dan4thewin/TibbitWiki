title: HotKeys
tags: Reference
created: 2026-04-01T22:00:00Z
modified: 2026-04-01T22:00:00Z

## Keyboard shortcuts

| Key | Action |
|-----|--------|
| `/` | Open search bar |
| `q` | Close bottom note (pop the stack) |
| `a` | Create new note |
| `e` | Open topmost note in external editor |
| `Shift-E` | Open topmost note in browser textarea |
| `Tab` | Jump to next note card |
| `Shift-Tab` | Jump to previous note card |
| `#` | Add tag to topmost note |
| `Ctrl-G` | Cancel / dismiss (search, tag input, edit) |
| `Escape` | Cancel / dismiss (same as Ctrl-G) |

Shortcuts only fire when no input or textarea is focused.
All keys are configurable per-wiki in `config.yaml` under
`theme.shared` (or `theme.{wiki}` for overrides):

```yaml
theme:
  shared:
    key_search: "/"
    key_close: q
    key_edit: e
    key_textarea: E
    key_new: a
    key_next: Tab
    key_prev: Shift-Tab
    key_tag: "#"
```

Set any key to `""` to disable it.

---

## Vimium compatibility

[Vimium](https://addons.mozilla.org/en-US/firefox/addon/vimium-ff/)
adds vim-style navigation to Firefox and Chrome/Edge. TibbitWiki
is designed to coexist with it:

1. In Vimium settings, add a per-site rule to pass TibbitWiki's
   keys through:

   | URL pattern | Excluded keys |
   |-------------|---------------|
   | `http://localhost:8080/*` | `/aqe#` |

2. Vimium's `f` key shows **link hints** on all clickable elements —
   sidebar items, search results, wiki word links, tag pills, and
   tab headers. This replaces the need for a built-in link hint system.

3. Other useful Vimium keys that work alongside TibbitWiki:
   - `f` / `F` — open link in current / new tab
   - `d` / `u` — scroll half page down / up
   - `gg` / `G` — scroll to top / bottom
   - `yy` — copy current URL
   - `H` / `L` — browser back / forward
   - `?` — show all Vimium shortcuts
