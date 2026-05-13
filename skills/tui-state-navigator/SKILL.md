---
name: tui-state-navigator
description: Use this skill when validating, exploring, or repairing terminal TUI workflows with tmux captures, especially when button/order changes can break keyboard navigation. It learns and stores state transitions in a project-local JSON state map.
---

# TUI State Navigator

Use this skill for terminal UIs where a visual state must be checked through real keyboard interaction rather than by reading code alone.

The skill is generic. It can drive any terminal TUI that can run inside tmux, as long as the project provides a state map JSON with a launch command, state classifiers, transitions, and safe learning candidates.

The core pattern is:

1. Run the TUI inside tmux.
2. Capture the screen.
3. Classify the current screen into a named state using visible markers.
4. Choose a transition toward the target state.
5. Send keys.
6. Capture and verify the predicted next state.
7. If a transition fails, learn a replacement key sequence and store it in the state map.

## Project Tools

In a project that bundles this skill, prefer these tools:

- `scripts/tui-state-navigator.js`: state-machine navigator with optional learning.
- a state map JSON, for example `scripts/tui-state-map.json`: persisted command, state definitions, transitions, markers, and candidate key sequences.

## Standard Workflow

Start with a full current-state check. If the project map contains `command`, no `--cmd` is needed:

```bash
node scripts/tui-state-navigator.js --all --wait 1 --key-wait 0.05 --width 120 --height 24
```

For another TUI, provide its map and command:

```bash
node scripts/tui-state-navigator.js --map path/to/tui-map.json --cmd "python -m textual_app" --all
```

For a specific target:

```bash
node scripts/tui-state-navigator.js --target mcp.transfer --trace --wait 1 --key-wait 0.05
```

When focus/highlight matters, include ANSI capture and focus reporting:

```bash
node scripts/tui-state-navigator.js --target mcp.transfer --trace --ansi --show-focus
```

When a target fails because a menu option moved, run learning mode:

```bash
node scripts/tui-state-navigator.js --target mcp.transfer --learn --trace --wait 1 --key-wait 0.05
```

Learning mode tries candidate key sequences from the source state. If one reaches the expected target state, it updates the selected state map with the new `keys`, `learned: true`, and `learnedAt`.

After learning, inspect the diff before trusting it:

```bash
git diff -- path/to/tui-state-map.json
node scripts/tui-state-navigator.js --map path/to/tui-state-map.json --all --wait 1 --key-wait 0.05
```

## Adding New States

If the screen is classified as `unknown`, add a state entry to the active state map.

Use stable visible markers:

- Prefer panel titles, page titles, and persistent labels.
- Avoid values from local user config, counts, timestamps, paths, wrapped phrases, or selected row names.
- Put specific states before broad overview states.
- Use `exclude` markers when an overview page shares the same title as modal/editor states.

Then add or learn transitions into that state.

## Highlight And Focus Checks

Use `selectedText` on a transition when the current highlighted item matters:

```json
{
  "from": "home",
  "to": "settings",
  "keys": ["Down", "Enter"],
  "markers": ["Settings"],
  "selectedText": "Home"
}
```

The navigator captures tmux with ANSI style, extracts likely highlighted/focused text from reverse-video, bold/background styling, and pointer glyphs, then validates `selectedText` before sending keys. Classification and ordinary markers still use stripped plain text so ANSI codes do not break matching.

If a TUI does not expose highlight styling through tmux, focus detection can be empty. In that case `selectedText` falls back to checking that the expected text exists on the plain screen; keep `markers` precise for those states.

## Creating A Map For Another TUI

Create a JSON file with:

```json
{
  "version": 1,
  "command": "your-tui-command",
  "cwd": ".",
  "start": "home",
  "states": [
    {"name": "home", "include": ["Home"]},
    {"name": "settings", "include": ["Settings"]}
  ],
  "transitions": [
    {"from": "home", "to": "settings", "keys": ["Down", "Enter"], "markers": ["Settings"]}
  ],
  "candidates": {
    "default": [["Enter"], ["Down", "Enter"], ["Tab"], ["Esc"]]
  }
}
```

Then run:

```bash
node scripts/tui-state-navigator.js --map path/to/map.json --all --trace
```

## Repair Rules

Before changing code, distinguish these failure types:

- `navigation marker missing`: a source-page option was removed, renamed, wrapped badly, or hidden by viewport size.
- `state prediction failed`: keys were sent, but the next state did not match the expected state.
- `expected initial state dashboard, got unknown`: startup failed or the classifier does not recognize the first screen.
- `can't find pane`: the TUI process exited; inspect the tmux command, `TMPDIR`, build errors, or command output.

If learning finds a new key sequence, keep the update only if the final captured state is semantically correct.

## Safety

Learning mode should only be used on non-destructive navigation states unless the state map candidate list has been constrained.

Do not add broad candidates like `Enter` on screens where the highlighted action may save, delete, export, launch, or run a transfer. For those states, add explicit safe candidates under `candidates.<state>` in the JSON map.

## Output Expectations

When reporting results, include:

- Target(s) checked.
- Whether all targets reached.
- Any learned transition and the exact new key sequence.
- Any state-map files changed.
- Any remaining unknown or unsafe states.

For detailed schema notes, see `references/state-map-schema.md`.
