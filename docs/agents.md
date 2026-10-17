---
title: Agents
section: Agents
order: 4
description: Claude Code and other agents in hive, their status, and how they come back after a restart.
---

# Agents

hive knows which of your terminals run coding agents, what each one is doing,
and which ones are waiting for you. After a restart it resumes them where they
were.

## Setup

For Claude Code, install hive's hooks once:

```sh
hive agent install claude
```

This adds six hooks to `~/.claude/settings.json` and leaves everything else in
the file alone. Running it again changes nothing. To remove them:

```sh
hive agent uninstall claude
```

Claude Code runs hooks only in folders you trust. Start new Claude sessions
inside hive to use them. Outside hive, the hooks exit at once.

## What hive learns

| Claude Code event | State |
| --- | --- |
| A session starts, or `/clear` | idle, and its session ID |
| You send a prompt | working |
| A tool finishes | working |
| It asks for permission | blocked, with the reason |
| It finishes a turn | done |
| The session ends | ended |
| You're back at the shell prompt | ended |

The last row covers an agent that was killed before it could say it ended:
when the pane's shell is back in front, the agent has exited. A report from
it later, once it's running again, brings it back.

The agent's task comes from the terminal title Claude Code sets, such as
"Migrate rate limiter to Redis".

`hive agents` lists them, blocked first:

```
STATE    HARNESS  SESSION      BLOCK   DIR          SINCE   MESSAGE
blocked  claude   api-gateway  …a29f3  ~/src/api    now     Claude needs your permission to use Bash
working  claude   hive-demo    agent   ~/src/hive   2m ago
```

The app shows the same in the agents sidebar, and sends a notification when an
agent becomes blocked or finishes while the app is in the background.

## Resume

When the server starts again after a stop, a crash or a reboot, each agent's
pane runs its harness's resume command with the latest session ID:

| Harness | Command |
| --- | --- |
| Claude Code | `claude --resume <id>` |
| Codex | `codex resume <id>` |
| OpenCode | `opencode -s <id>` |
| Gemini | `gemini --resume <id>` |
| Grok | `grok --resume <id>` |

An agent whose session ended is not resumed, and neither is one whose pane
was back at the shell prompt when the server stopped. If a resume fails, the pane stays
open so you can read why.

To change a command, set `HIVE_RESUME_<HARNESS>` to a JSON list in the
server's environment. `{id}` is replaced by the session ID:

```sh
export HIVE_RESUME_CLAUDE='["claude","--resume","{id}","--model","opus"]'
```

## Other harnesses

Only Claude Code has hooks hive installs. Grok runs Claude Code's hooks too
(it reads `~/.claude/settings.json`), and hive knows: grok's sessions are
reported as Grok, so they resume with `grok --resume`. A grok subagent's
session starting or ending doesn't change the agent in the pane.

More generally, when the program running in a pane is a different agent than
the one a report names, hive goes by the program. Codex, OpenCode and Gemini
are resumed when they report a session ID through `agent.report`. See the
[server API](./api).
