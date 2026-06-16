# TUI Component Reuse TODO

This document tracks reusable TUI patterns found across Profile Manager, Prompt Manager, MCP Manager, Skill Manager, and Dashboard. It is intentionally a TODO list, not an implementation plan for immediate refactoring.

## Current Shared Pieces

- `internal/tui/form_widgets.go`
  - `renderCompactFormRow`: shared compact label/value form row used by Profile Manager and Prompt Manager.
  - `renderSelectModalOverlay`: shared single-select modal currently used by Prompt Manager.

## High Priority

- [ ] Reuse compact form rows in MCP Manager form mode.
  - Current duplication: `internal/tui/mcp_manager_model.go` renders editor rows with label, divider, input style, focus style, and value truncation inline.
  - Candidate target: replace the form-mode row rendering in `renderEditor()` with `renderCompactFormRow`.
  - Caveat: MCP fields have custom select rendering and hints; keep field-specific value formatting local.
  - Acceptance: MCP form snapshots stay visually equivalent, and `go test ./internal/tui` passes.

- [ ] Reuse compact read-only rows for MCP details and Prompt details.
  - Current duplication: `renderMCPConfigRow()` and `promptManagerModel.detailRow()` both render read-only label/value rows.
  - Candidate target: add a small wrapper around `renderCompactFormRow` for read-only detail rows, or use `renderCompactFormRow` directly with `ReadOnly: true`.
  - Caveat: MCP uses a narrower label width in some contexts; support custom label width only if needed after measuring snapshots.
  - Acceptance: detail panes keep stable alignment across MCP and Prompt snapshots.

- [ ] Reuse select modal overlay in Profile Manager simple selects.
  - Current duplication: Profile Manager provider type modal and API type modal manually build option rows with cursor and confirm/cancel help.
  - Candidate target: use `renderSelectModalOverlay` for single-select provider type.
  - Caveat: API type is multi-select, so do not force it through a single-select helper. Add a separate multi-select modal helper only if another multi-select surface appears.
  - Acceptance: provider type selection keeps existing keyboard behavior and tests in `profile_manager_*` continue to pass.

## Medium Priority

- [ ] Extract draft save state handling for manager UIs.
  - Current duplication risk: Prompt Manager now keeps source vs draft config, dirty state, explicit save, and dirty quit confirmation locally. Other manager surfaces may need the same staged-edit workflow.
  - Candidate target: a small helper around dirty state, save status text, and quit confirmation semantics, while keeping manager-specific config cloning and persistence callbacks local.
  - Caveat: Profile and MCP managers still intentionally save many actions immediately, so do not force staged saves into surfaces that do not need them.
  - Acceptance: Prompt Manager behavior remains unchanged and any future staged manager can share the same confirmation/save vocabulary.

- [ ] Extract reusable action button row rendering.
  - Current duplication: Profile Manager, Prompt Manager, and MCP Manager each render compact action buttons with active/focused states and wrapping or spacing logic.
  - Candidate target: a helper that renders `[]action{key,label}` into rows using `pmLeftBtnStyle`, `pmLeftActiveBtnStyle`, `pmCompactBtnStyle`, and `pmCompactActiveBtnStyle`.
  - Caveat: left-pane actions and right-pane save/test actions have different layout constraints; start with left-pane action rows only.
  - Acceptance: Prompt actions and Profile left actions remain visually stable in snapshots.

- [ ] Extract two-pane shell layout helper.
  - Current duplication: Profile Manager, Prompt Manager, Skill Manager, and Dashboard all build header, two panels, status/footer, focus styling, and viewport clipping manually.
  - Candidate target: a helper that accepts header text/rendered header, left/right content, widths, focus side, footer, and viewport height.
  - Caveat: Dashboard has custom detail styling, Skill Manager has modal-like right-pane states, and MCP has denser custom layout; do not overgeneralize.
  - Acceptance: at least two managers use the helper without adding manager-specific branches to the helper.

- [ ] Extract confirmation prompt state handling.
  - Current duplication: Prompt Manager, MCP Manager, and Skill Manager each keep confirmation state and handle `Y/N/Esc` similarly.
  - Candidate target: a small `confirmState` helper with message, active flag, and `handleConfirmKey` result.
  - Caveat: delete actions have manager-specific side effects; keep callbacks outside the helper.
  - Acceptance: confirm/cancel key behavior remains covered by manager tests.

- [ ] Standardize status and help sections.
  - Current duplication: managers render status summaries, log/details sections, and footer help text with similar style choices.
  - Candidate target: helpers for status line rendering and footer rendering, not one global status model.
  - Caveat: MCP has richer diagnostics, Profile has test summaries, Prompt has validation statuses; keep semantic state local.
  - Acceptance: no behavior changes, only shared rendering functions.

## Low Priority

- [ ] Consider a reusable scroll window/list helper.
  - Current duplication: Profile list windowing, Prompt preset/binding lists, Skill installed list, and MCP server list all manage selected rows and visible windows differently.
  - Candidate target: pure helper for `start/end/showUp/showDown` and row metadata.
  - Caveat: current list behaviors differ enough that a broad component may be premature.
  - Acceptance: only extract if another list gains scrolling or row hit testing.

- [ ] Consider shared modal keyboard navigation helpers.
  - Current duplication: modal `up/down/tab/shift+tab/enter/esc` cursor handling is repeated in Profile Manager and Prompt Manager.
  - Candidate target: pure cursor movement helpers for single-select modals.
  - Caveat: Profile models modal has search, edit, fetch, and scroll behavior; keep that specialized.
  - Acceptance: no loss of custom model modal behaviors.

- [ ] Consider shared snapshot state setup conventions.
  - Current duplication: `Render*Snapshot` functions instantiate models, set dimensions, and force state-specific UI modes.
  - Candidate target: consistent naming and helper for size normalization and model dimension assignment.
  - Caveat: snapshot state construction is mostly explicit and readable today; do not hide important setup behind a generic dispatcher.
  - Acceptance: snapshot code remains easy to audit.

## Do Not Extract Yet

- Profile Manager model selection modal.
  - It has search, API fetch, add/edit/delete, default model selection, and scroll behavior. This is a specialized tool, not a generic select.

- MCP raw YAML/JSON editor.
  - Its cursor and text rendering are editor-specific and should stay local unless another raw text editor appears.

- Dashboard menu styling.
  - It is small, readable, and uses dashboard-specific styles. Extraction would add indirection without clear reuse.

## Suggested Refactor Order

1. MCP form rows -> `renderCompactFormRow`.
2. Prompt and MCP detail rows -> shared read-only row helper.
3. Profile provider type select -> `renderSelectModalOverlay`.
4. Left-pane action button rows.
5. Confirmation prompt helper.

Each step should include focused snapshot coverage plus `go test ./internal/tui` before moving to the next item.
