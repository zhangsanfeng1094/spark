# MCP Manager UX Redesign Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Upgrade the terminal MCP Manager so it prioritizes quick server creation, clearer browse-mode status, and visible actions without changing MCP config behavior.

**Architecture:** Keep `mcpManagerModel` as the central state holder, but split browse-mode rendering into explicit sections and introduce focus zones for `Quick Add`, `Server List`, and `Actions`. Reuse existing editor and probe logic, then layer a more guided layout, contextual help text, and stronger status summaries on top.

**Tech Stack:** Go, Bubble Tea, Lip Gloss, Cobra, Go testing package

---

## File Map

- Modify: `internal/tui/mcp_manager_model.go`
  Purpose: browse-mode state, focus zones, key handling, section rendering, contextual status bar, empty state, actions, and editor entry behavior.
- Modify: `internal/tui/mcp_manager_test.go`
  Purpose: view- and behavior-level tests covering quick add, empty state, focus-zone navigation, diagnostics summaries, and contextual help.
- Modify: `internal/tui/prompt.go` only if shared styles are required and can be reused safely.
  Purpose: optional shared style adjustments. Avoid if `mcp_manager_model.go` can remain self-contained.
- Reference: `docs/superpowers/specs/2026-04-09-mcp-manager-design.md`
  Purpose: approved design source for UX and interaction behavior.

### Task 1: Add Browse-Mode Focus Zones

**Files:**
- Modify: `internal/tui/mcp_manager_model.go`
- Test: `internal/tui/mcp_manager_test.go`

- [ ] **Step 1: Write the failing tests**

Add tests that assert:

- a new manager starts with focus in `Quick Add`
- `Tab` moves focus from `Quick Add` to `Server List` to `Actions`
- `Shift+Tab` cycles backwards
- `Enter` on a quick-add template opens the editor in add mode with the expected transport

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/tui -run 'TestMCPManager(BrowseFocus|QuickAdd)'`
Expected: FAIL because focus-zone state and quick-add activation do not exist yet.

- [ ] **Step 3: Write minimal implementation**

In `internal/tui/mcp_manager_model.go`:

- add a browse focus enum for `quick add`, `server list`, and `actions`
- add default quick-add items for `stdio`, `sse`, and `http`
- initialize browse focus to `quick add`
- update browse-mode key handling for `Tab`, `Shift+Tab`, arrows, and `Enter`
- map `Enter` on quick-add items to `startAddEditor(transport)`

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/tui -run 'TestMCPManager(BrowseFocus|QuickAdd)'`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/tui/mcp_manager_model.go internal/tui/mcp_manager_test.go
git commit -m "feat: add mcp manager browse focus zones"
```

### Task 2: Redesign Left Pane for Quick Add and Richer Server Rows

**Files:**
- Modify: `internal/tui/mcp_manager_model.go`
- Test: `internal/tui/mcp_manager_test.go`

- [ ] **Step 1: Write the failing tests**

Add tests that assert:

- the left pane renders a `Quick Add` section
- quick-add templates display `stdio`, `sse`, and `http`
- the server list renders transport metadata and status badges
- selected and focused rows use different visible markers

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/tui -run 'TestMCPManager(RenderQuickAdd|RenderServerRow)'`
Expected: FAIL because the existing left pane only renders plain text sections and one-line rows.

- [ ] **Step 3: Write minimal implementation**

In `internal/tui/mcp_manager_model.go`:

- replace the current left-pane text block with sectioned rendering
- render quick-add rows with label and short hint
- render server rows with badge, transport, disabled/probing state, and clearer selection markers
- preserve keyboard shortcuts in footer copy only where still useful

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/tui -run 'TestMCPManager(RenderQuickAdd|RenderServerRow)'`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/tui/mcp_manager_model.go internal/tui/mcp_manager_test.go
git commit -m "feat: redesign mcp manager left pane"
```

### Task 3: Add Guided Empty State and Structured Right-Pane Overview

**Files:**
- Modify: `internal/tui/mcp_manager_model.go`
- Test: `internal/tui/mcp_manager_test.go`

- [ ] **Step 1: Write the failing tests**

Add tests that assert:

- empty state shows `No MCP servers yet`
- empty state suggests starter actions
- selected server overview shows transport, enabled state, and command or URL
- probe metadata such as last probe and tools count remain visible

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/tui -run 'TestMCPManager(RenderEmptyState|RenderOverview)'`
Expected: FAIL because the current right pane uses a minimal plain-text fallback and unstructured details.

- [ ] **Step 3: Write minimal implementation**

In `internal/tui/mcp_manager_model.go`:

- add a dedicated empty-state renderer
- split the details pane into overview and diagnostics sections
- render operational summary fields before long-form text
- keep editor rendering on the existing edit path

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/tui -run 'TestMCPManager(RenderEmptyState|RenderOverview)'`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/tui/mcp_manager_model.go internal/tui/mcp_manager_test.go
git commit -m "feat: add structured mcp manager details pane"
```

### Task 4: Rework Diagnostics Into Conclusion-First Status Messaging

**Files:**
- Modify: `internal/tui/mcp_manager_model.go`
- Test: `internal/tui/mcp_manager_test.go`

- [ ] **Step 1: Write the failing tests**

Add tests that assert:

- broken probes show a `Current State` style summary
- diagnostics include a concise `Why` explanation
- suggested fixes render under a distinct next-action heading
- disabled servers surface an intentional disabled message

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/tui -run 'TestMCPManager(RenderDiagnostics|DisabledStatus)'`
Expected: FAIL because diagnostics are currently a flat detail paragraph with bullet suggestions.

- [ ] **Step 3: Write minimal implementation**

In `internal/tui/mcp_manager_model.go`:

- add small helpers to format `Current State`, `Why`, and `Next Action`
- render probe failures by stage with concise explanations
- cap suggested fixes to a tight, scan-friendly list
- keep existing status summary logic as the data source where possible

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/tui -run 'TestMCPManager(RenderDiagnostics|DisabledStatus)'`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/tui/mcp_manager_model.go internal/tui/mcp_manager_test.go
git commit -m "feat: improve mcp manager diagnostics"
```

### Task 5: Add Visible Action Group and Contextual Status Bar

**Files:**
- Modify: `internal/tui/mcp_manager_model.go`
- Test: `internal/tui/mcp_manager_test.go`

- [ ] **Step 1: Write the failing tests**

Add tests that assert:

- browse mode renders a visible action group in the right pane
- status bar help text changes based on active browse focus
- edit mode status bar only shows edit-relevant commands
- delete confirm mode still overrides status styling and guidance

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/tui -run 'TestMCPManager(RenderActions|StatusBarContext)'`
Expected: FAIL because the current implementation uses a single static status hint string.

- [ ] **Step 3: Write minimal implementation**

In `internal/tui/mcp_manager_model.go`:

- add a rendered actions section for browse mode
- compute contextual help text from current mode and browse focus
- preserve delete-confirm error emphasis
- keep legacy hotkeys functional while reducing footer clutter

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/tui -run 'TestMCPManager(RenderActions|StatusBarContext)'`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/tui/mcp_manager_model.go internal/tui/mcp_manager_test.go
git commit -m "feat: add contextual actions to mcp manager"
```

### Task 6: Tighten Create/Edit Flow Without Changing Config Semantics

**Files:**
- Modify: `internal/tui/mcp_manager_model.go`
- Test: `internal/tui/mcp_manager_test.go`

- [ ] **Step 1: Write the failing tests**

Add tests that assert:

- add mode for `stdio` shows command, args, and env fields but hides URL
- add mode for `http` or `sse` shows URL but hides stdio-only fields
- edit mode keeps overview as the default selected-server experience until edit is invoked
- editor footer shows `Save`, `Save & Probe`, and `Cancel` guidance

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/tui -run 'TestMCPManager(EditorFlow|VisibleFieldsFollowTransport)'`
Expected: FAIL for any new browse-vs-edit expectations and possibly PASS for existing transport visibility assertions that should remain green.

- [ ] **Step 3: Write minimal implementation**

In `internal/tui/mcp_manager_model.go`:

- preserve the existing transport-aware visible field logic
- improve add-mode copy and editor headings
- ensure selected-server browse mode stays separate from edit mode
- render short field hints where they remove ambiguity without adding noise

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/tui -run 'TestMCPManager(EditorFlow|VisibleFieldsFollowTransport)'`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/tui/mcp_manager_model.go internal/tui/mcp_manager_test.go
git commit -m "feat: streamline mcp manager editor flow"
```

### Task 7: Run Full Relevant Verification

**Files:**
- Modify: none unless regressions appear
- Test: `internal/tui/mcp_manager_test.go`

- [ ] **Step 1: Run targeted TUI tests**

Run: `go test ./internal/tui`
Expected: PASS

- [ ] **Step 2: Run broader application tests that exercise MCP command wiring**

Run: `go test ./internal/app ./internal/config`
Expected: PASS

- [ ] **Step 3: Fix regressions if needed**

If failures appear, add a failing test first for the specific regression, then make the minimal fix and re-run the affected package tests.

- [ ] **Step 4: Re-run the full relevant test set**

Run:

```bash
go test ./internal/tui
go test ./internal/app ./internal/config
```

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/tui/mcp_manager_model.go internal/tui/mcp_manager_test.go
git commit -m "test: verify mcp manager redesign"
```
