#!/usr/bin/env node

const fs = require('fs');
const path = require('path');
const { spawnSync } = require('child_process');

const rootDir = path.resolve(__dirname, '..');

function usage() {
  console.log(`Usage:
  scripts/tui-state-navigator.js --target <state> [options]
  scripts/tui-state-navigator.js --all [options]

Options:
  --map <path>          State map JSON (default: scripts/tui-state-map.json)
  --cmd <command>       Command to run inside tmux (default: map.command)
  --cwd <path>          Working directory for commands (default: current directory)
  --target <state>      Target state to reach
  --all                 Validate all known states
  --learn               Repair failed transitions by trying candidate keys and saving the map
  --width <cols>        tmux pane width (default: 120)
  --height <rows>       tmux pane height (default: 32)
  --wait <seconds>      Wait after launch and transitions (default: 1)
  --key-wait <seconds>  Wait between keys (default: 0.1)
  --trace               Print intermediate captures
  --ansi                Preserve ANSI style in trace/final output and force color in the TUI
  --show-focus          Print detected highlighted/focused text for each capture
  --list                List known states
  -h, --help            Show this help`);
}

function parseArgs(argv) {
  const args = {
    map: path.join(rootDir, 'scripts/tui-state-map.json'),
    cmd: '',
    cwd: '',
    target: '',
    all: false,
    learn: false,
    width: 120,
    height: 32,
    wait: 1,
    keyWait: 0.1,
    trace: false,
    ansi: false,
    showFocus: false,
    list: false
  };

  for (let i = 0; i < argv.length; i++) {
    const arg = argv[i];
    switch (arg) {
      case '--map':
        args.map = path.resolve(process.cwd(), argv[++i] || '');
        break;
      case '--cmd':
        args.cmd = argv[++i] || '';
        break;
      case '--cwd':
        args.cwd = path.resolve(argv[++i] || '.');
        break;
      case '--target':
        args.target = argv[++i] || '';
        break;
      case '--all':
        args.all = true;
        break;
      case '--learn':
        args.learn = true;
        break;
      case '--width':
        args.width = Number(argv[++i] || 120);
        break;
      case '--height':
        args.height = Number(argv[++i] || 32);
        break;
      case '--wait':
        args.wait = Number(argv[++i] || 1);
        break;
      case '--key-wait':
        args.keyWait = Number(argv[++i] || 0.1);
        break;
      case '--trace':
        args.trace = true;
        break;
      case '--ansi':
        args.ansi = true;
        break;
      case '--show-focus':
        args.showFocus = true;
        break;
      case '--list':
        args.list = true;
        break;
      case '-h':
      case '--help':
        usage();
        process.exit(0);
        break;
      default:
        throw new Error(`Unsupported argument: ${arg}`);
    }
  }

  return args;
}

function run(command, options = {}) {
  const result = spawnSync(command, {
    cwd: options.cwd || rootDir,
    shell: true,
    encoding: 'utf8',
    env: { ...process.env, TMPDIR: '/tmp' },
    maxBuffer: 1024 * 1024 * 20,
    ...options
  });
  return {
    status: result.status,
    stdout: result.stdout || '',
    stderr: result.stderr || ''
  };
}

function shellQuote(value) {
  return `'${String(value).replace(/'/g, `'\\''`)}'`;
}

function sleep(seconds) {
  if (seconds <= 0) return;
  run(`sleep ${Number(seconds)}`);
}

function loadMap(file) {
  const map = JSON.parse(fs.readFileSync(file, 'utf8'));
  map.__file = file;
  map.__dir = path.dirname(file);
  map.__rawCwd = map.cwd;
  if (map.cwd && !path.isAbsolute(map.cwd)) {
    map.cwd = path.resolve(map.__dir, map.cwd);
  }
  return map;
}

function saveMap(file, map) {
  map.updatedAt = new Date().toISOString();
  const copy = { ...map };
  if (copy.__rawCwd !== undefined) {
    copy.cwd = copy.__rawCwd;
  }
  delete copy.__file;
  delete copy.__dir;
  delete copy.__rawCwd;
  fs.writeFileSync(file, `${JSON.stringify(copy, null, 2)}\n`);
}

function classify(map, screen) {
  for (const state of map.states || []) {
    const include = state.include || [];
    const exclude = state.exclude || [];
    if (include.every((marker) => screen.includes(marker)) && exclude.every((marker) => !screen.includes(marker))) {
      return state.name;
    }
  }
  return 'unknown';
}

function findTransition(map, from, to) {
  return (map.transitions || []).find((transition) => transition.from === from && transition.to === to);
}

function findPath(map, target) {
  const start = map.start || 'dashboard';
  if (target === start) return [];

  const queue = [{ state: start, path: [] }];
  const seen = new Set([start]);

  while (queue.length > 0) {
    const item = queue.shift();
    for (const transition of map.transitions || []) {
      if (transition.from !== item.state || seen.has(transition.to)) continue;
      const nextPath = item.path.concat(transition);
      if (transition.to === target) return nextPath;
      seen.add(transition.to);
      queue.push({ state: transition.to, path: nextPath });
    }
  }
  return null;
}

function keysLabel(keys) {
  return (keys || []).join(' ');
}

function stripANSI(value) {
  return String(value).replace(/\x1b\][^\x07]*(\x07|\x1b\\)/g, '').replace(/\x1b\[[0-?]*[ -/]*[@-~]/g, '');
}

function sgrState(codes, state) {
  const next = { ...state };
  const parts = codes.length > 0 ? codes.split(';').map((part) => (part === '' ? 0 : Number(part))) : [0];
  for (const code of parts) {
    if (code === 0) {
      next.active = false;
      next.reverse = false;
      next.bold = false;
      next.fg = '';
      next.bg = '';
    } else if (code === 1) {
      next.active = true;
      next.bold = true;
    } else if (code === 7) {
      next.active = true;
      next.reverse = true;
    } else if (code === 22) {
      next.bold = false;
    } else if (code === 27) {
      next.reverse = false;
    } else if (code >= 30 && code <= 37) {
      next.active = true;
      next.fg = String(code);
    } else if (code >= 40 && code <= 47) {
      next.active = true;
      next.bg = String(code);
    } else if (code >= 90 && code <= 97) {
      next.active = true;
      next.fg = String(code);
    } else if (code >= 100 && code <= 107) {
      next.active = true;
      next.bg = String(code);
    }
  }
  next.active = next.reverse || next.bold || next.bg !== '';
  return next;
}

function normalizeFocusText(value) {
  return stripANSI(value).replace(/[│╭╮╰╯─┃┌┐└┘═>➤◆●○()[\]]/g, ' ').replace(/\s+/g, ' ').trim();
}

function extractFocusTexts(ansiScreen) {
  const lines = String(ansiScreen).split(/\r?\n/);
  const focused = [];
  for (const line of lines) {
    let state = { active: false, reverse: false, bold: false, fg: '', bg: '' };
    let segment = '';
    let plainLine = '';
    let activeLine = '';
    for (let i = 0; i < line.length;) {
      if (line[i] === '\x1b') {
        const match = line.slice(i).match(/^\x1b\[([0-?;]*)[ -/]*[@-~]/);
        if (match) {
          if (segment && state.active) activeLine += segment;
          segment = '';
          state = sgrState(match[1], state);
          i += match[0].length;
          continue;
        }
      }
      const ch = line[i];
      plainLine += ch;
      segment += ch;
      i++;
    }
    if (segment && state.active) activeLine += segment;

    const pointerLine = /[>➤◆]\s*\S/.test(plainLine) ? plainLine : '';
    for (const candidate of [activeLine, pointerLine]) {
      const text = normalizeFocusText(candidate);
      if (text && !focused.includes(text)) focused.push(text);
    }
  }
  return focused;
}

function tmuxSessionName(prefix) {
  return `${prefix}-${process.pid}-${Math.random().toString(36).slice(2, 8)}`;
}

function startSession(args, session) {
  const colorEnv = args.ansi ? '-u NO_COLOR FORCE_COLOR=1 CLICOLOR_FORCE=1 COLORTERM=truecolor ' : '';
  const command = `tmux new-session -d -x ${Number(args.width)} -y ${Number(args.height)} -s ${shellQuote(session)} ${shellQuote(`env ${colorEnv}TMPDIR=/tmp ${args.cmd}`)}`;
  const result = run(command, { cwd: args.cwd });
  if (result.status !== 0) {
    throw new Error(result.stderr || result.stdout || `failed to start tmux session ${session}`);
  }
  sleep(args.wait);
}

function killSession(session) {
  run(`tmux kill-session -t ${shellQuote(session)} >/dev/null 2>&1 || true`);
}

function capture(session, styled = true) {
  const result = run(`tmux capture-pane -t ${shellQuote(session)} -p${styled ? ' -e' : ''}`);
  const raw = result.status !== 0 ? result.stdout + result.stderr : result.stdout;
  return {
    raw,
    plain: stripANSI(raw),
    focus: extractFocusTexts(raw)
  };
}

function sendKeys(session, keys, args) {
  for (const key of keys || []) {
    const result = run(`tmux send-keys -t ${shellQuote(session)} ${shellQuote(key)}`);
    if (result.status !== 0) {
      throw new Error(result.stderr || result.stdout || `failed to send key ${key}`);
    }
    sleep(args.keyWait);
  }
}

function assertMarkers(screen, transition) {
  for (const marker of transition.markers || []) {
    if (!screen.plain.includes(marker)) {
      throw new Error(`navigation marker missing for ${transition.from} -> ${transition.to}: ${marker}`);
    }
  }
  if (transition.selectedText) {
    const expected = Array.isArray(transition.selectedText) ? transition.selectedText : [transition.selectedText];
    const ok = expected.some((marker) => screen.focus.some((text) => text.includes(marker)));
    if (!ok) {
      if (screen.focus.length === 0 && expected.some((marker) => screen.plain.includes(marker))) {
        return;
      }
      throw new Error(`focus marker missing for ${transition.from} -> ${transition.to}: ${expected.join(' | ')}; detected focus: ${screen.focus.join(' || ') || '(none)'}`);
    }
  }
}

function traceCapture(enabled, label, state, screen) {
  if (!enabled) return;
  console.log(`\n=== ${label}: ${state} ===`);
  if (screen.focus.length > 0) {
    console.log(`focus: ${screen.focus.join(' || ')}`);
  }
  console.log(globalThis.__tuiNavigatorAnsi ? screen.raw : screen.plain);
}

function runPath(map, args, transitions, options = {}) {
  const session = tmuxSessionName('spark-tui-nav');
  const prefixKeys = [];

  try {
    startSession(args, session);
    let screen = capture(session, true);
    let current = classify(map, screen.plain);
    traceCapture(args.trace, 'initial', current, screen);
    if (args.showFocus && !args.trace) {
      console.log(`focus: ${screen.focus.join(' || ') || '(none)'}`);
    }

    const expectedStart = map.start || 'dashboard';
    if (current !== expectedStart) {
      throw new Error(`expected initial state ${expectedStart}, got ${current}`);
    }

    for (const transition of transitions) {
      if (current !== transition.from) {
        throw new Error(`expected source state ${transition.from}, got ${current}`);
      }
      assertMarkers(screen, transition);
      console.log(`step: ${transition.from} -> ${transition.to} via [${keysLabel(transition.keys)}]`);
      sendKeys(session, transition.keys || [], args);
      prefixKeys.push(...(transition.keys || []));
      sleep(args.wait);

      screen = capture(session, true);
      current = classify(map, screen.plain);
      traceCapture(args.trace, transition.to, current, screen);
      if (args.showFocus && !args.trace) {
        console.log(`focus: ${screen.focus.join(' || ') || '(none)'}`);
      }
      if (current !== transition.to) {
        return {
          ok: false,
          failedTransition: transition,
          prefixKeys: prefixKeys.slice(0, prefixKeys.length - (transition.keys || []).length),
          actual: current,
          screen
        };
      }
    }

    return { ok: true, state: current, screen };
  } finally {
    killSession(session);
  }
}

function runCandidate(map, args, prefixKeys, candidateKeys) {
  const session = tmuxSessionName('spark-tui-learn');
  try {
    startSession(args, session);
    sendKeys(session, prefixKeys, args);
    sleep(args.wait);
    sendKeys(session, candidateKeys, args);
    sleep(args.wait);
    const screen = capture(session, true);
    return { state: classify(map, screen.plain), screen };
  } finally {
    killSession(session);
  }
}

function candidateKeys(map, from) {
  const candidates = (map.candidates && (map.candidates[from] || map.candidates.default)) || [];
  return candidates.map((keys) => keys.slice());
}

function repairTransition(map, args, failed, prefixKeys) {
  console.log(`learning: repairing ${failed.from} -> ${failed.to}`);
  for (const candidate of candidateKeys(map, failed.from)) {
    const result = runCandidate(map, args, prefixKeys, candidate);
    console.log(`try [${keysLabel(candidate)}] => ${result.state}`);
    if (result.state === failed.to) {
      failed.keys = candidate;
      failed.learned = true;
      failed.learnedAt = new Date().toISOString();
      console.log(`learned: ${failed.from} -> ${failed.to} via [${keysLabel(candidate)}]`);
      return true;
    }
  }
  return false;
}

function validateTarget(map, args, target) {
  let path = findPath(map, target);
  if (!path) {
    throw new Error(`no path to target: ${target}`);
  }

  for (let attempt = 0; attempt < 2; attempt++) {
    const result = runPath(map, args, path);
    if (result.ok) {
      console.log(`ok: reached ${target}`);
      if (!args.trace) {
        console.log(`\n=== final: ${target} ===`);
        if (result.screen.focus.length > 0) {
          console.log(`focus: ${result.screen.focus.join(' || ')}`);
        }
        console.log(args.ansi ? result.screen.raw : result.screen.plain);
      }
      return true;
    }

    const failed = result.failedTransition;
    console.log(`state prediction failed: expected ${failed.to}, got ${result.actual}`);
    if (!args.learn) {
      console.log(args.ansi ? result.screen.raw : result.screen.plain);
      return false;
    }
    if (!repairTransition(map, args, failed, result.prefixKeys)) {
      console.log(args.ansi ? result.screen.raw : result.screen.plain);
      return false;
    }
    saveMap(args.map, map);
    path = findPath(map, target);
  }

  return false;
}

function main() {
  const args = parseArgs(process.argv.slice(2));
  const map = loadMap(args.map);
  globalThis.__tuiNavigatorAnsi = args.ansi;
  args.cwd = args.cwd || map.cwd || map.__dir || process.cwd();
  args.cmd = args.cmd || map.command || '';

  if (args.list) {
    for (const state of map.states || []) console.log(state.name);
    return;
  }

  if (!args.all && !args.target) {
    throw new Error('Use --target <state> or --all.');
  }
  if (args.all && args.target) {
    throw new Error('Use either --target or --all, not both.');
  }
  if (!args.cmd) {
    throw new Error('No TUI command configured. Set command in the map or pass --cmd.');
  }

  const targets = args.all ? (map.states || []).map((state) => state.name) : [args.target];
  let failures = 0;

  for (const target of targets) {
    console.log(`\n### checking ${target} ###`);
    if (!validateTarget(map, args, target)) failures++;
  }

  if (failures > 0) {
    throw new Error(`failed targets: ${failures}`);
  }
  if (args.all) {
    console.log(`ok: all ${targets.length} targets reached`);
  }
}

try {
  main();
} catch (error) {
  console.error(error.message);
  process.exit(1);
}
