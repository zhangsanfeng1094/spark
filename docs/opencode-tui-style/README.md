# OpenCode TUI Style Reference

This folder stores the captured OpenCode TUI structure used as a reference for a Spark TUI restyle.

- `opencode-tui-state-map.json`: navigator-compatible state map for the observed OpenCode home, agent toggle, and command palette states.
- `style-tokens.md`: visual tokens and component-state notes translated from the captured tmux screens.

Validate the state map with:

```bash
node scripts/tui-state-navigator.js --map docs/opencode-tui-style/opencode-tui-state-map.json --list
node scripts/tui-state-navigator.js --map docs/opencode-tui-style/opencode-tui-state-map.json --target home.plan --trace --wait 1 --key-wait 0.05
node scripts/tui-state-navigator.js --map docs/opencode-tui-style/opencode-tui-state-map.json --target commands.palette --trace --wait 1 --key-wait 0.05
```

The map is intentionally narrow. It captures only low-risk navigation states that were directly observed: Build home, Plan home, and the command palette.
