---
title: Scripts
section: Automate
order: 22
description: Run TypeScript on the server to set up panes, react to events and drive your agents.
---

# Scripts

A script is a TypeScript or JavaScript file the hive server runs. It calls
the same methods as the CLI and the apps, and hears the same events. You
don't need Node, Bun or Deno: hive strips the types and runs the code
itself.

```ts
// tests.ts: open a split running the tests, and wait for the result.
const pane = await h.block();
const [tests] = await pane.new_split({ layout: { command: ["make", "test"] }, focus: false });
const { exit_code } = await tests.wait();
console.log(exit_code === 0 ? "tests pass" : "tests fail");
export default () => exit_code;
```

```sh
hive script run --allow spawn tests.ts
```

The log goes to stderr, and what the default export returns goes to stdout
as JSON. A script that throws exits with status 1.

## Running a script

| Command | Does |
| --- | --- |
| `hive script run FILE [KEY=VALUE…]` | Run a file once. `KEY=VALUE` pairs become `h.args`. |
| `hive script run -e CODE` | Run code from the command line |
| `hive script run -` | Run code from stdin |
| `hive script stop RUN_ID` | Stop a running script. Ctrl-C does the same for the one you started. |
| `hive script ls` | Running scripts, and the latest that ended |
| `hive script check FILE` | Report errors without running it |

Options for `run`:

| Option | Does |
| --- | --- |
| `--allow PERMS` | What the script may do, such as `control,spawn`. Without it, it can only read. |
| `--args JSON` | Arguments as a JSON object, read as `h.args` |
| `--for DUR` | Stop it after this long, such as `10m` |
| `--all` | Hear events from every session, not only its own |

Inside a hive terminal, a script runs in that pane's session: methods that
need a `session_id` or a `block_id` get the current ones when you leave them
out. Outside hive, name them with `-s` and `-b`, as other commands do.

The CLI bundles the script on your machine, so `hive -S box script run
./x.ts` works with your local files. The server only runs scripts sent from
other machines when you turn on `script-remote-run` in its
[settings](./settings).

## What a script can do

Everything is on `h`. Names are the API's own, so `hive api describe`
explains each one.

| Write | Does |
| --- | --- |
| `await h.session.list()` | Any method, by its name: `h.session.new_split(…)`, `h.agent.list()`, `h.whoami()` |
| `await h.terminal.format({ args: { tail: 5 } })` | Terminal methods, without the `hive.` prefix |
| `await h.call("session.view", {…})` | The same, with the method's name as a string |
| `await h.block()` / `h.block("logs")` | The current block, or one by target: `write`, `send_key`, `format`, `wait`, `new_split`, `label`, `zoom`, `focus`, `close`, `agent` |
| `await h.session_of()` | The current session: `label`, `view`, `new_window`, `close` |
| `h.on("terminal.bell", fn)` | Run `fn(ctx, event)` for every event of that name. `h.on("*", fn)` hears them all. |
| `h.on("terminal.agent_changed", { state: "blocked" }, fn)` | Only events whose fields match. A list matches any of its values. |
| `await h.wait("terminal.child_exited", { block_id }, { timeout: "1m" })` | Wait for one event |
| `h.after("5s", fn)`, `h.every("1m", fn)`, `await h.sleep("200ms")` | Timers |
| `console.log(…)`, `h.log("warn", …)` | Write to the script's log |
| `h.args`, `h.ctx` | The arguments, and where the script runs (`session_id`, `block_id`) |
| `h.stop()` | End the script now |

Events use short names: session events by their own name
(`window_created`), terminal events with `terminal.` in front
(`terminal.child_exited`, `terminal.agent_changed`). Their fields are those
in [the API's events](./api#events), with `block_id` and `session_id` added.
In `terminal.agent_changed`, `session_id` is the agent's own session and
`hive_session_id` is hive's.

A script ends when its code and everything it waits for have finished. One
that registers `h.on` handlers or `h.every` timers keeps running until you
stop it, it calls `h.stop()`, or `--for` runs out.

A failed call throws a `HiveError` with the API's `code` and `status`:

```ts
try {
  await h.session.create({ command: ["make"] });
} catch (e) {
  if (e.code === "permission_denied") console.log("run me with --allow spawn");
}
```

## Permissions

A script can only do what it was allowed. Without `--allow`, it can read:
list sessions, describe blocks, hear events.

| Permission | Allows |
| --- | --- |
| `read` | Sessions, windows, blocks, agents, the server's details, events. Every script has it. |
| `screen` | What terminals show: `format`, and the folders `list_dir` reads |
| `control` | Arrange, label, focus and close; new panes with your shell, in any folder |
| `spawn` | New panes that run a command. Includes `control`. |
| `input` | Typing into terminals: `write` and `send_key` |
| `config` | Changing settings, except `listen` and those starting with `script` |
| `server` | Stopping the server, saving now, and the settings `config` can't change |

`hive api describe METHOD` shows the permission each method needs.

A script can also declare what it needs, so you don't pass `--allow` every
time:

```ts
export default hive.script({
  name: "label-by-folder",
  permissions: ["control"],
  async setup(h) {
    const pane = await h.block();
    await pane.label((await pane.pwd()).split("/").pop()!);
  },
});
```

A script that starts another can't give it more than it has itself.

## Safety

Scripts run as you, inside the server. Permissions protect you from a
script's mistakes, not from someone who can already reach your server: that
is what the Unix socket and your tailnet are for.

Everything a script changes (arranging, running, typing, settings) goes to
an audit log, `scripts/audit.jsonl` in the server's state folder. Text a
script types is logged as its length and a hash, not the text.

Imports must be your own files (`./lib`), not packages.
