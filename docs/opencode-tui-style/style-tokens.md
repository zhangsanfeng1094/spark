# OpenCode TUI Style Tokens

Captured from `opencode --pure /home/fxh/mycode/spark` in a 120x32 tmux pane.

## Visual Direction

- Overall: quiet terminal workspace, almost no chrome, no card-heavy dashboard.
- Background: full-screen near-black canvas.
- Focus: one saturated blue accent line on the left edge of the input block.
- Text: white for primary labels, medium gray for hints, blue for active agent, amber for tips.
- Layout: centered logo and input surface with a bottom status line pinned to the terminal edges.
- Borders: avoid rounded boxes for primary surfaces; use one vertical rule and a low-contrast bottom rule.

## Palette

| Token | Hex | Observed Use |
| --- | --- | --- |
| `ocBg` | `#0a0a0a` | full terminal background |
| `ocPanelBg` | `#1e1e1e` | prompt/input block background |
| `ocPanelBgSubtle` | `#282828` | logo shadow and dark glyph fill |
| `ocPanelBgRaised` | `#434343` | bright logo shadow fill |
| `ocText` | `#eeeeee` | logo, model name, shortcuts |
| `ocTextStrong` | `#ffffff` | inherited primary terminal foreground |
| `ocMuted` | `#808080` | placeholder, inactive labels, path/version |
| `ocAccent` | `#5c9cf5` | input left rail, active agent label |
| `ocWarning` | `#f5a742` | tip bullet and label |

## Component States

### App Shell

- Fill the viewport with `ocBg`; do not wrap the whole UI in a panel.
- Keep horizontal page margin small, around 2 columns.
- Bottom status line uses `ocMuted` on `ocBg`, with path on the left and version/status on the right.

### Logo

- Centered horizontally in the first third of the viewport.
- Render as block/ASCII text, not a bordered title bar.
- Use `ocMuted` for the left/dim word segment and `ocText` for the right/bright segment.

### Prompt Block

- Width: about 76 columns at 120-column terminal width.
- Position: centered horizontally, below the logo.
- Background: `ocPanelBg`.
- Left rail: a single `ocAccent` vertical rule (`┃`) ending with `╹`.
- Bottom rule: low-contrast block/line glyph, not a box border.
- Placeholder: `ocMuted`, example text inline.
- Agent/model row: active agent in `ocAccent`, separator in `ocMuted`, model in `ocText`, provider/context in `ocMuted`.

### Shortcuts

- Place shortcut hints just under the prompt block, right aligned to the block.
- Shortcut key names use `ocText`; descriptions use `ocMuted`.
- Keep labels terse, for example `tab agents` and `ctrl+p commands`.

### Tip Row

- Centered below the prompt surface.
- Leading bullet and `Tip` label use `ocWarning`; body uses `ocMuted`; important term uses `ocText`.
- Tip should not be inside a card.

### Command Palette

- Overlay-like centered list without a full boxed modal border.
- Header row: title on the left, escape hint on the right.
- Search row is plain text on the same background.
- Group labels such as `Suggested` and `Session` use muted/secondary styling.
- Rows are dense: command label left, shortcut right, no large vertical padding.

## Spark Implementation Notes

- Current Spark styles in `internal/tui/profile_manager_model.go` use purple focus (`#b58cff`) and rounded panel borders. To approximate OpenCode, start by replacing dashboard/profile focus surfaces with an unboxed shell and a blue rail focus treatment.
- Prefer changing shared color tokens first, then only the dashboard/prompt-like entry screens. Managers with dense forms may keep panel structure until the new visual language is proven.
- Keep state markers stable: `Ask anything`, active agent label (`Build`/`Plan`), `tab agents`, `ctrl+p commands`, and `Commands` are good classifier markers.
