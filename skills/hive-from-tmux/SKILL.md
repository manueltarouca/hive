---
name: hive-from-tmux
description: Move a user's work from tmux into hive, one window at a time. Recreate sessions, windows and splits, and resume Claude Code and other coding agents in the same conversation, without losing anything. Use it when the user wants to switch from tmux to hive, migrate or port their tmux sessions, or hand over running agents to hive, even if they only say "move my tmux stuff over".
---

# Moving from tmux to hive

A tmux window can hold running agents, long jobs, and half-typed commands.
Moving it should lose none of them, and the user must be able to stop at any
point. So:

- **Read tmux, never change it,** except for the one window being moved, and
  only after the user agrees. tmux stays the fallback until the user is done.
- **Move one window at a time,** and check each before the next.
- **Never run the same agent conversation twice.** Two Claude processes
  writing one session corrupt it. Exit the tmux copy first, then resume it in
  hive.
- **Move the window you are running in last,** or ask the user to run you from
  somewhere else. Exiting your own pane ends this conversation.

The skills `hive` (driving hive) and `hive-setup` (installing it) cover the
basics. Check that `hive` works before you start.

## 1. Take stock (read-only)

```sh
tmux list-sessions -F '#{session_name}'
tmux list-windows -a -F '#{session_name}:#{window_index} #{window_name} #{window_layout}'
tmux list-panes -a -F '#{session_name}:#{window_index}.#{pane_index} #{pane_pid} #{pane_current_command} #{pane_current_path} #{pane_width}x#{pane_height}'
```

**What runs in each pane.** `pane_current_command` is often unclear: Claude
Code shows as a version number such as `2.1.294`. Look at the pane's
children instead:

```sh
ps -o pid,args -p "$(pgrep -P PANE_PID | head -1)"
```

Note each agent's full command line. Flags such as `--model` or
`--dangerously-skip-permissions` must come back with it.

**The window you are in.** If `TMUX_PANE` is set, that pane is you:
`tmux display -p -t "$TMUX_PANE" '#{session_name}:#{window_index}'`.

**Some windows stay in tmux.** A moved pane gets a new process. Anything
whose value lives in the running process is lost by moving it, for example:
- a long job or migration in the middle of its run
- an SSH session or a REPL holding state
- a process that can't simply be started again, such as one holding a lock,
  a tunnel, a `caffeinate`, or a foreground job the user is watching

Plan those as **stays in tmux**, and never stop them to move them. When
unsure, ask. A partial move is a normal outcome, not a failure.

Show the user a table with these columns, and agree on what moves:
- tmux window
- what runs in each pane
- the plan for each pane: resume the agent, re-run the command, stays in
  tmux, or plain
  shell

## 2. Find each agent's session

Resuming needs the agent's session ID or name.

1. **Already in its command line.** `claude --resume NAME_OR_ID` means you
   use that.
2. **Claude transcripts.** Otherwise, Claude keeps one file per session in
   `~/.claude/projects/<dir>/<session-id>.jsonl`. `<dir>` is the pane's cwd
   with every character other than letters and digits replaced by `-`.
   `/Users/me/src/app` becomes `-Users-me-src-app`.
   - With one Claude in that folder, the newest file is usually it.
   - With several, take a distinctive line from the pane
     (`tmux capture-pane -p -t PANE -S -200`), such as the user's last
     prompt, and grep the transcripts for it.
3. **Still unsure?** Ask the user, or have them run `/status` in that pane,
   which shows the session ID.

Codex, OpenCode, Gemini and Grok have their own resume commands
(`codex resume ID`, `opencode -s ID`, `gemini --resume ID`,
`grok --resume ID`). Find their IDs the same way: from the command line
first, then their own session files. Ask when in doubt.

## 3. Build the hive side

One hive session per tmux session. Create it once, in the first window's
directory:

```sh
hive new SESSION --cwd DIR             # creates the session and its first window
hive -s SESSION label --window NAME    # name the first window like the tmux one
```

Each later tmux window becomes a hive window:

```sh
hive -s SESSION run --label NAME --cwd DIR --focus=false
```

**Split panes.** Recreate them with `split`, side by side (`right`) or
stacked (`below`). Read the shape from `#{window_layout}`: `{…}` is
side-by-side and `[…]` is stacked. `--ratio` is the new pane's share in
percent.

```sh
hive -s SESSION -b BLOCK split --split right --ratio 50 --cwd DIR --focus=false
```

Start every pane as a **shell**, then type the command into it. If the
program exits, the pane stays open, and the agent's hooks can report its
session:

```sh
hive -b BLOCK send 'claude --resume ID --dangerously-skip-permissions'
hive -b BLOCK send-key enter
```

## 4. Move one agent

For each agent pane, in order:

1. **Confirm with the user.** Make sure the agent is idle, not in the middle
   of a turn: look at `tmux capture-pane -p -t PANE | tail -20`. A busy agent
   loses its turn when it exits.
2. **Exit it in tmux:**
   `tmux send-keys -t PANE '/exit' Enter`
   Wait until the pane shows a shell again.
3. **Resume it in hive** with its exact original flags, as in step 3.
4. **Check it** before the next one. After a few seconds,
   `hive -b BLOCK capture --trim | tail -30` should show the old conversation
   and a prompt.
   - If the hive hooks are installed, `hive agents` lists it.
   - If the resume failed, the hive pane still has its shell. Read the error
     and fix it, or resume it in tmux again. Nothing is lost.

Other programs:
- **Easy to restart** (dev servers, watchers, `tail -f`): with the user's OK,
  stop them in tmux (`ctrl+c`), then start them in hive.
- **Can't restart without loss:** they stay in tmux, as agreed in step 1.
- **Plain shells:** need nothing; their directory is enough.

## 5. Finish

- **Tell the user what moved and what stayed in tmux, and why**, as a short
  list. The windows that stayed can move later, once their process
  finishes.
- **Leave the old tmux windows open** for the user to close when happy. Their
  scrollback is still useful. Don't run `tmux kill-session` yourself.
- **Your own window goes last.** Tell the user the command that resumes this
  conversation in hive, and let them run it after exiting you.

## Restores

hive resumes agents after a restart with `claude --resume ID`, but without
your extra flags. If an agent needs flags, set them for the server, for
example in the shell profile:

```sh
export HIVE_RESUME_CLAUDE='["claude","--resume","{id}","--dangerously-skip-permissions"]'
```

Then restart the server once with `hive server stop`, so it picks the change
up.
