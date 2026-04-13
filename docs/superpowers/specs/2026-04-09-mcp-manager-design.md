# MCP Manager UX Redesign

Date: 2026-04-09
Status: Draft for user review

## Goal

Redesign the terminal MCP Manager so it feels like a practical management surface instead of a placeholder screen. The new design should improve:

- first impression and visual hierarchy
- speed of adding a new MCP server
- clarity of health status, diagnostics, and next actions

The primary workflow is creating a new server. Existing server inspection and editing remain important secondary workflows.

## Non-Goals

- changing MCP config schema
- changing probe behavior or transport semantics
- introducing mouse support
- replacing the Bubble Tea and Lip Gloss stack

## Current Problems

The current screen works functionally, but several issues reduce usability:

- The left pane treats creation as plain text instead of a first-class action area.
- The right pane is a long text block with weak hierarchy.
- Empty state is cold and low guidance.
- Status information is present but not scannable.
- Most actions depend on memorizing hotkeys.
- Editing starts from a low-level form/raw mindset instead of a guided task flow.

## Design Principles

1. Optimize for first-run success.
2. Keep expert shortcuts, but make the visible UI sufficient on its own.
3. Prefer status summaries and recommended actions over verbose diagnostics.
4. Use stronger hierarchy without turning the terminal UI into a box-heavy dashboard.
5. Preserve the current model structure where possible to keep implementation risk moderate.

## Proposed Experience

### Layout

Keep the two-column layout, but reorganize both panes.

Left pane sections:

- `Quick Add`
- `Your Servers`
- contextual shortcut hints

Right pane states:

- empty state
- server overview
- diagnostics
- actions
- editor

This preserves familiarity while making create, inspect, and act feel distinct.

### Left Pane

#### Quick Add

The top of the left pane becomes the default entry point. It contains:

- `Common Templates`
- `Custom`

Initial templates:

- `stdio`
- `sse`
- `http`

Each template row should show:

- name
- short purpose hint
- transport type

Example intent:

- `stdio` for local command-backed servers
- `sse` for remote stream endpoints
- `http` for request-based MCP endpoints

Future template presets can be added later without changing the navigation model.

#### Your Servers

Server rows become denser and more informative. Each row should show:

- server name
- status badge
- transport label
- disabled state if applicable
- probing indicator if active

Use a two-line layout when width allows:

- line 1: server name + status badge
- line 2: transport + short secondary state

The selected row should stand out clearly. Focus and selection should remain visually distinct.

### Right Pane

#### Empty State

When no server exists, show a guided panel instead of plain text:

- what this manager is for
- recommended first action
- the three starter entry points
- the shortest possible path to success

Example structure:

- headline: `No MCP servers yet`
- subtext: `Start with a template, save the config, then probe connectivity.`
- actions: `Create stdio`, `Create SSE`, `Create HTTP`

#### Overview

When a server is selected, the top section summarizes the current object:

- name
- transport
- enabled state
- command or URL
- args count if relevant
- env count if relevant
- last probe timestamp
- latency
- tools detected

This section should read like an operational summary, not raw config.

#### Diagnostics

Diagnostics should shift from narrative text to conclusion-first messaging.

Order:

1. `Current State`
2. `Why`
3. `Next Action`

Examples:

- `Healthy: server responded to tools/list`
- `Broken: command not found`
- `Disabled intentionally`
- `Configured but unverified`

Below the state headline, show:

- failure stage if any
- concise cause
- up to 2-3 suggested fixes

#### Actions

The right pane should always expose available actions visibly. Proposed actions:

- `Probe`
- `Edit`
- `Duplicate` if implemented during this redesign, otherwise omit
- `Enable` or `Disable`
- `Delete`
- `Sync`
- `Import`

These actions should be rendered as an explicit action group rather than hidden in the footer.

## Navigation Model

Introduce explicit focus zones in browse mode:

- `Quick Add`
- `Server List`
- `Actions`

Navigation rules:

- `Tab` and `Shift+Tab` move between focus zones
- arrow keys move within the current zone
- `Enter` activates the current item

Keep existing single-key accelerators as power-user shortcuts:

- `a`, `s`, `h` for direct add
- `e` edit
- `p` probe current
- `r` probe all
- `d` delete
- `i` import
- `y` sync
- `q` quit

This creates a dual-mode interaction model:

- visible focus-driven navigation for discoverability
- mnemonic hotkeys for speed

## Create and Edit Flow

### Create Flow

The primary path should be:

1. select a template in `Quick Add`
2. open a minimal form for that transport
3. fill required fields only
4. `Save` or `Save & Probe`

Default create behavior:

- first focus lands in `Quick Add`
- `Enter` on a template opens the form
- the form shows only fields relevant to the chosen transport

Transport-specific field visibility:

- `stdio`: name, enabled, command, args, env, disabled reason
- `sse` or `http`: name, enabled, URL, disabled reason

The create form should prioritize minimal friction over completeness.

### Edit Flow

Selecting a server should not immediately force edit mode. The default right pane remains overview plus diagnostics.

Editing starts only after explicit action.

Edit mode should provide:

- focused form mode
- optional raw mode for advanced users
- clear save actions
- field hints for ambiguous inputs

Suggested field hint examples:

- `Command`: executable to launch
- `Args`: one argument per line
- `Env`: `KEY=value`, one per line

Raw mode stays available, but it becomes a secondary path rather than a competing default.

## Visual Direction

The visual direction should feel like a serious terminal tool, not a novelty interface.

Guidelines:

- strengthen type hierarchy with restrained use of bold and accent color
- use badges for status instead of embedding status in paragraphs
- reduce large undifferentiated text blocks
- avoid excessive border nesting
- make empty states and action groups feel intentional

Color semantics should stay limited:

- healthy
- warning
- error
- disabled
- accent
- dim text

Status bar behavior should become contextual instead of exhaustive.

Examples:

- browse mode: show navigation and action hints relevant to the active focus zone
- edit mode: show save/cancel/probe guidance only
- destructive confirm mode: show confirm/cancel guidance only

## Error Handling

The redesign should preserve existing safety behavior:

- delete remains confirm-gated
- invalid form input still blocks save
- raw parse errors still surface clearly

New UI behavior should make failures easier to recover from:

- failed save should keep the user oriented in the editor
- failed probe should highlight the failure stage and recommended next action
- empty or invalid required fields should read as direct form errors, not buried status text

## Testing Strategy

Add or extend view-level tests around:

- empty state rendering
- left pane quick-add rendering
- server row status rendering
- focus-zone dependent help text
- transport-specific editor field visibility
- diagnostics summaries for common statuses

Do not snapshot the entire interface. Prefer targeted assertions on meaningful strings and states to keep tests stable.

## Implementation Plan Shape

The implementation should likely proceed in three passes:

1. Restructure browse-mode state and rendering
2. Improve create and edit flow
3. Tighten contextual help and diagnostics presentation

This keeps the interface usable after each pass and lowers regression risk.

## Risks

- The current model is list-selection centric, so adding focus zones will touch input handling.
- More expressive rendering can become noisy if styles are overused.
- If action groups and help text both expose the same commands poorly, the UI may feel redundant instead of clearer.

## Recommendation

Implement the redesign as an iterative refactor of `internal/tui/mcp_manager_model.go` and its related tests, not as a rewrite. The current model already contains enough status and editor state to support the new interaction model with moderate structural changes.
