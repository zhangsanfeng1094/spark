# MCP Manager Left Pane Density Tuning

Date: 2026-04-10
Status: Draft for user review

## Goal

Reduce visual crowding in the left side of the terminal MCP Manager without changing the core navigation or edit flow.

The target outcome is a more balanced two-column layout where:

- the left pane gets enough width to feel readable
- the `Actions` section stays visible but lighter
- server rows remain two-line entries, but the secondary line becomes much shorter

## Non-Goals

- changing browse focus zones or keybindings
- changing server data, probe behavior, or save logic
- redesigning the right pane
- adding new actions or templates

## Current Problem

The current left pane feels cramped for three reasons:

- the pane width is fixed at a narrow value
- `Actions` and `Your Servers` compete for the same limited space
- both action rows and server rows spend too much vertical and horizontal space on repeated detail text

This makes the server list harder to scan than it should be.

## Design Principles

1. Keep the current information hierarchy.
2. Improve scanability before adding any new information.
3. Spend extra width on the server list, not on decorative spacing.
4. Preserve the current focus model and selection cues.

## Proposed Change

### Layout Balance

Increase the left pane width by one step so it no longer feels compressed against the right pane.

Implementation intent:

- raise the left pane width from the current fixed value to a wider fixed value in the normal case
- keep the existing guard that prevents the left pane from taking more than half of the viewport
- preserve the minimum width protection for the right pane

This keeps the change simple and predictable while improving readability immediately.

### Actions Section

Keep `Actions` at the top of the left pane, but compress each row so the section reads like a lightweight tool strip instead of a competing content block.

Each action item should:

- keep the existing label
- keep a short explanation
- avoid repeated long description text

The goal is to preserve discoverability while reducing noise.

### Server Rows

Keep server rows as two-line entries.

Row structure:

- line 1: status badge + server name
- line 2: transport + short state flags

The second line should only contain short operational tags such as:

- `stdio`
- `http`
- `disabled`
- `probing`

Avoid longer narrative descriptions in the row itself. Detailed explanation remains in the right pane.

## Rendering Rules

### Action Rows

Action rows should render with a single concise summary line beneath the title, or an equivalently short compact rendering if the current item style makes that cleaner.

The text should stay short enough that the wider pane feels calmer rather than fuller.

### Server Secondary Line

Construct the secondary line from:

1. transport label
2. optional short flags joined with ` • `

Examples:

- `stdio`
- `stdio • disabled`
- `http • probing`
- `sse • disabled`

This keeps the second line useful without making the list visually heavy.

## Testing

Update the existing MCP manager rendering tests to verify:

- the left pane still includes `Actions` and `Your Servers`
- server rows still expose the transport label
- focused and selected markers still render correctly

No new interaction tests are required unless the layout change forces a rendering helper split.

## Implementation Scope

Only touch the MCP manager TUI rendering surface:

- `internal/tui/mcp_manager_model.go`
- `internal/tui/mcp_manager_test.go`

No changes are planned for probe execution, config storage, or editor behavior.
