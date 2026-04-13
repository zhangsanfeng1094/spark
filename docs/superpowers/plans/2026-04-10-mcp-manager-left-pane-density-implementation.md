# MCP Manager Left Pane Density Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the MCP manager left pane less crowded by widening the pane and compressing action/server row rendering without changing interaction behavior.

**Architecture:** Keep the current two-pane Bubble Tea layout and adjust only the left-pane width calculation plus the row render helpers. Preserve the existing browse focus model, selection styles, and right-pane behavior while tightening content density in `renderQuickAddItem` and `renderServerRow`.

**Tech Stack:** Go, Bubble Tea, Lip Gloss, Go test

---

### Task 1: Lock down left-pane rendering expectations

**Files:**
- Modify: `internal/tui/mcp_manager_test.go`
- Test: `internal/tui/mcp_manager_test.go`

- [ ] **Step 1: Write the failing tests**

Add assertions that:
- the full view widens the left pane enough to show a longer action summary without clipping the whole section structure
- quick-add rows no longer repeat the description text twice
- server rows still render a compact second line containing transport and short flags

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/tui -run 'TestMCPManager(RenderQuickAddSection|RenderServerRowIncludesTransportAndSelectionMarker|ViewLayout)'`
Expected: FAIL because the current quick-add rows repeat descriptions and the current width/layout does not match the new assertions.

### Task 2: Implement compact left-pane rendering

**Files:**
- Modify: `internal/tui/mcp_manager_model.go`
- Test: `internal/tui/mcp_manager_test.go`

- [ ] **Step 1: Write minimal implementation**

Change the left-pane width calculation and row render helpers so that:
- the left pane is wider in standard viewports
- quick-add items render a shorter secondary line
- server rows keep a two-line layout but use only transport plus short flags on the second line

- [ ] **Step 2: Run focused tests to verify they pass**

Run: `go test ./internal/tui -run 'TestMCPManager(RenderQuickAddSection|RenderServerRowIncludesTransportAndSelectionMarker|ViewLayout)'`
Expected: PASS

- [ ] **Step 3: Run the broader MCP manager test set**

Run: `go test ./internal/tui -run 'TestMCPManager'`
Expected: PASS
