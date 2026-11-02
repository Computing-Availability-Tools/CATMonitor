#!/usr/bin/env node
// Install CATMonitor Agent Skills (skills/<name>/SKILL.md) into an agent
// skills directory. Zero dependencies; Node >= 14.
//
// Usage:
//   npx catmonitor-skills                 # user-level ~/.claude/skills
//   npx catmonitor-skills --project       # ./.claude/skills of the cwd
//   npx catmonitor-skills --dest DIR      # custom destination
//   npx catmonitor-skills --list          # preview only, install nothing
//
// The user-level ~/.claude/skills target is read by Claude Code, OpenCode and
// other Agent Skills compatible agents. From the CATMonitor repo you can also
// use `make skills-install` or scripts/install_skills.sh (bash).

'use strict';

const fs = require('fs');
const os = require('os');
const path = require('path');

// Skills payload lives next to this script's package root (skills/).
const PAYLOAD_DIR = path.join(__dirname, '..', 'skills');

function usage() {
  process.stdout.write(
    [
      'Usage: catmonitor-skills [--dest DIR | --project] [--list]',
      '',
      'Install CATMonitor Agent Skills for Claude Code / OpenCode.',
      '',
      'Options:',
      '  --dest DIR    Install into DIR (created if missing).',
      '  --project     Install into ./.claude/skills of the current directory.',
      '  --list        Only list what would be installed; install nothing.',
      '  -h, --help    Show this help.',
      '',
      'Default (no options): user-level ~/.claude/skills.',
      ''
    ].join('\n')
  );
}

function parseArgs(argv) {
  const opts = { mode: 'user', listOnly: false, dest: '' };
  for (let i = 0; i < argv.length; i++) {
    const arg = argv[i];
    if (arg === '--dest') {
      const value = argv[i + 1];
      if (!value) {
        process.stderr.write('error: --dest requires a value\n');
        process.exit(2);
      }
      opts.mode = 'custom';
      opts.dest = value;
      i++;
    } else if (arg === '--project') {
      opts.mode = 'project';
    } else if (arg === '--list') {
      opts.listOnly = true;
    } else if (arg === '-h' || arg === '--help') {
      usage();
      process.exit(0);
    } else {
      process.stderr.write('error: unknown option: ' + arg + '\n');
      usage();
      process.exit(2);
    }
  }
  return opts;
}

function collectSkills(payloadDir) {
  let entries;
  try {
    entries = fs.readdirSync(payloadDir, { withFileTypes: true });
  } catch (err) {
    process.stderr.write('error: skills payload not found: ' + payloadDir + ' (' + err.message + ')\n');
    process.exit(1);
  }
  const skills = entries
    .filter((e) => e.isDirectory())
    .map((e) => path.join(payloadDir, e.name))
    .filter((dir) => fs.existsSync(path.join(dir, 'SKILL.md')));
  if (skills.length === 0) {
    process.stderr.write('error: no skills with SKILL.md found under ' + payloadDir + '\n');
    process.exit(1);
  }
  return skills;
}

function rmRf(target) {
  // Node >= 14.14 has rmSync; fall back for older 14.x.
  if (typeof fs.rmSync === 'function') {
    fs.rmSync(target, { recursive: true, force: true });
    return;
  }
  if (!fs.existsSync(target)) return;
  fs.rmdirSync(target, { recursive: true });
}

function copyDir(src, dst) {
  fs.mkdirSync(dst, { recursive: true });
  for (const entry of fs.readdirSync(src, { withFileTypes: true })) {
    const from = path.join(src, entry.name);
    const to = path.join(dst, entry.name);
    if (entry.isDirectory()) {
      copyDir(from, to);
    } else if (entry.isFile()) {
      fs.copyFileSync(from, to);
    }
  }
}

function main() {
  const opts = parseArgs(process.argv.slice(2));

  let dest;
  if (opts.mode === 'project') {
    dest = path.join(process.cwd(), '.claude', 'skills');
  } else if (opts.mode === 'custom') {
    dest = opts.dest;
  } else {
    dest = path.join(os.homedir(), '.claude', 'skills');
  }

  const skills = collectSkills(PAYLOAD_DIR);

  if (opts.listOnly) {
    process.stdout.write('Would install ' + skills.length + ' skill(s) into: ' + dest + '\n');
    for (const dir of skills) {
      process.stdout.write('  ' + path.basename(dir) + '  ->  ' + path.join(dest, path.basename(dir)) + '\n');
    }
    return;
  }

  process.stdout.write('Installing ' + skills.length + ' skill(s) into: ' + dest + '\n');
  fs.mkdirSync(dest, { recursive: true });
  for (const dir of skills) {
    const name = path.basename(dir);
    const target = path.join(dest, name);
    // Replace any previous copy wholesale so removed files do not linger.
    rmRf(target);
    copyDir(dir, target);
    process.stdout.write('  installed: ' + name + '\n');
  }

  process.stdout.write(
    [
      '',
      'Done. Skills are active for new agent sessions. Try prompts like:',
      '  - "帮我体检一下这台服务器"        (catmonitor-health)',
      '  - "查一下 NPU 的温度和利用率"     (catmonitor-metrics)',
      '  - "最近有没有掉卡记录？"          (catmonitor-faults)',
      '',
      'Uninstall: rm -rf ' + path.join(dest, 'catmonitor-*'),
      ''
    ].join('\n')
  );
}

main();
