---
title: Agent setup
section: Start
order: 3
description: Let your coding agent install hive, learn its commands, and move your tmux work over.
---

# Agent setup

Your coding agent can set up hive for you, and then use it. Copy this prompt
and paste it into Claude Code, Codex, or any other agent:

```
Fetch and follow the instructions at https://manueltarouca.github.io/hive/agent-setup/prompt.md to set me up with hive.
```

The [instructions](../agent-setup/prompt.md) tell the agent to:

1. Install hive's skills.
2. Build and install hive, and connect Claude Code to it.
3. If you use tmux, ask you whether to move your sessions over.

The agent tells you before it changes your agent settings.

## The skills

| Skill | Teaches the agent to |
| --- | --- |
| `hive` | Run commands in terminals it can watch, read their output, wait for them, and check which agents need you |
| `hive-setup` | Install, update and remove hive |
| `hive-from-tmux` | Move tmux windows into hive one at a time, and resume each agent in the same conversation |

In Claude Code, you can install them yourself:

```sh
claude plugin marketplace add manueltarouca/hive
claude plugin install hive@hive
```

For other agents, copy the folders in
[`skills/`](https://github.com/manueltarouca/hive/tree/main/skills) to the
folder where your agent loads skills. For Codex, that is `~/.codex/skills`.

## Moving from tmux

The agent moves one window at a time:

1. It exits each agent in tmux.
2. It resumes the agent in hive, with the same conversation and the same
   flags.
3. It checks that the agent is back before it moves the next one.

It doesn't change or close anything else in tmux. You can stop at any point,
and tmux is still there.

Move the window the agent itself runs in last, or run the agent from
somewhere else.
