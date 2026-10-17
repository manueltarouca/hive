These are instructions from the hive project. They set up hive, a terminal multiplexer for agent-heavy work, and teach you to use it.

Do the steps yourself by running the commands. Before a step changes something outside the hive folder, such as the user's agent settings, tell the user what it changes.

1. Install the hive skills.
2. Install hive.
3. (Optional) Move the user's tmux work into hive.

---

## 1. Install the hive skills

There are three skills:
- `hive`: drive hive.
- `hive-setup`: install it.
- `hive-from-tmux`: move work over from tmux.

Use the section for your agent.

### Claude Code

```
claude plugin marketplace add manueltarouca/hive
claude plugin install hive@hive
```

Then tell the user to run `/reload-plugins` in Claude Code.

### Codex

```
git clone --depth 1 https://github.com/manueltarouca/hive /tmp/hive-skills
mkdir -p "${CODEX_HOME:-$HOME/.codex}/skills"
cp -R /tmp/hive-skills/skills/hive /tmp/hive-skills/skills/hive-setup /tmp/hive-skills/skills/hive-from-tmux "${CODEX_HOME:-$HOME/.codex}/skills/"
```

### Other agents

Copy the three folders in `skills/` of https://github.com/manueltarouca/hive to the folder where your agent loads skills. If your agent has no skills, read the three `SKILL.md` files and follow them as instructions.

---

## 2. Install hive

Follow the `hive-setup` skill. If you can't load skills yet, read it here:
https://raw.githubusercontent.com/manueltarouca/hive/main/skills/hive-setup/SKILL.md

It installs hive from a release (macOS on Apple Silicon, Linux x86_64) or builds it from source, puts it on `PATH`, checks that it works, and connects Claude Code with hooks.

---

## 3. (Optional) Move from tmux

If `tmux ls` lists sessions, ask the user whether to move them into hive now. If they agree, follow the `hive-from-tmux` skill. It moves one window at a time and leaves tmux as it is, so the user can stop at any point.

---

When you are done, tell the user:

```
┌─ hive is set up ─────────────────────────────────┐
│  ✓ Skills   <where they are>                     │
│  ✓ hive     <path to the binary>                 │
│  ✓ Hooks    <settings file, or "skipped">        │
│                                                  │
│  Start:  hive new work --cwd <a project folder>  │
└──────────────────────────────────────────────────┘
```

Docs: every page is also Markdown. Start at /llms.txt on the site you got this from.
