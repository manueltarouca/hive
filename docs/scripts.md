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
| `hive script ls` | Scripts in the folder, running one-shots, and the latest that ended |
| `hive script check FILE` | Report errors without running it |
| `hive script types [--out FILE]` | Write `hive.d.ts`, for your editor |

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

## Types in your editor

`hive script types` writes `hive.d.ts`: every method of the server you talk
to, with its arguments, its result and the permission it needs. Put it
beside your scripts with a `tsconfig.json`, and your editor completes `h.`
and flags a misspelled argument before you run anything:

```json
{
  "compilerOptions": { "target": "es2022", "module": "esnext", "strict": true, "noEmit": true, "lib": ["es2022"] },
  "include": ["*.ts"]
}
```

Run `hive script types` again after you update hive. hive doesn't check
types when it runs a script; `tsc --noEmit` does, if you want it to.

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

## Agents, notifications and badges

A script can tell you things, and mark panes:

```ts
await h.notify.send({ title: "Tests failed", body: "3 failures in api", target: h.ctx.block_id, level: "error" });
await h.status.set({ target: h.ctx.block_id!, id: "tests", state: "error", msg: "3 failing" });
```

Notifications reach every app, and the server keeps them for a day, so an
app that was closed shows what it missed. A script can send 10 a minute.
Badges show in the pane's header, on the Mac and the phone, those that need
you first; they go away when their script stops. Both need the `ui`
permission.

`h.agents.on_state` runs when an agent reaches a state, with its row from
`agent.list` (its folder, its label, what it's doing):

```ts
export default hive.script({
  name: "nag-when-blocked",
  permissions: ["ui"],
  setup(h) {
    h.agents.on_state("blocked", async (ctx, row) => {
      await h.sleep("10m");
      const now = (await h.agents.list()).find((a) => a.block_id === row.block_id);
      if (now && now.agent.revision === row.agent.revision) {
        await h.notify.send({ title: "Still waiting for you", body: now.agent.message ?? "", target: row.block_id });
      }
    });
  },
});
```

A rule answers what you'd answer the same way every time. When an agent
is blocked on what the rule describes, you get a notification with a
button; pressing it sends the answer, but only if the agent is still
waiting on the same thing:

```ts
export default hive.script({
  name: "approve-reads",
  permissions: ["input", "ui"],
  setup(h) {
    h.agents.rule({
      name: "approve-reads",
      when: { state: "blocked", message: "permission to use (Read|Grep|Glob)\\b", cwd: "~/src" },
      do: { send_key: ["enter"] },
    });
  },
});
```

A rule only answers permission prompts, unless its `when` names another
`kind`: a question an agent asks you is blocked too, and never matches by
accident. Rules don't act on their own; they only offer the button.

## Watching what terminals print

`h.trigger` runs for every line a terminal prints that matches a pattern.
Patterns are RE2, matched by the server; colors and other escape codes are
left out, and a spinner's redraws count as one line:

```ts
export default hive.script({
  name: "red-on-fail",
  permissions: ["screen", "ui"],
  setup(h) {
    h.trigger({ pattern: "^(FAIL|panic:)" }, async (ctx, m) => {
      await h.status.set({ target: m.block_id, id: "fail", state: "error", msg: m.line });
    });
    h.trigger({ pattern: "Listening on :(\\d+)" }, async (ctx, m) => {
      await h.status.set({ target: m.block_id, id: "port", state: "done", msg: "port " + m.groups[0] });
    });
  },
});
```

`h.wait_output({ pattern, timeout })` waits for one line. A trigger can
name a `block` or a `session`, `screen: "alt"` for full-screen programs, or
`mode: "snapshot"` to match what a full-screen program shows instead of
lines. `hive.terminal.search` finds lines printed before a script started.
Triggers need the `screen` permission.

## Actions

An action is something a script does when asked: by `hive api call
action.run`, by a notification's button, or by another script.

```ts
export default hive.script({
  name: "scratch",
  permissions: ["control"],
  setup(h) {
    h.actions.register({
      name: "scratch",
      title: "Open a scratch window",
      run: async (ctx) => {
        await h.session.new_window({ session_id: ctx.session_id!, label: "scratch" });
      },
    });
  },
});
```

An action runs with its script's permissions. Another script can run it
only when it's `public: true` and that script already has those
permissions itself.

## Keys

A script in the scripts folder can give an action a key, in the Mac app:

```ts
export default hive.script({
  name: "scratch-key",
  permissions: ["control", "ui"],
  setup(h) {
    h.actions.register({
      name: "scratch-window",
      title: "Open a scratch window",
      run: async (ctx) => { await h.session.new_window({ session_id: ctx.session_id!, label: "scratch" }); },
    });
    h.bind("ctrl+cmd+n", "scratch-window");
    h.bind("ctrl+b>c", "client.new_tab");               // a sequence: ctrl+b, then c
    h.mode("resize", { exclusive: true });              // keys only for this mode, until Escape
    h.bind("ctrl+cmd+r", "client.mode", { mode: "resize" });
    h.bind("resize/left", "client.resize", { direction: "left" });
  },
});
```

Keys are written as `cmd+shift+t`, with `ctrl`, `alt`, `shift` and `cmd`;
sequences as `ctrl+b>c`; keys in a mode as `resize/left`. A binding can
name a script's action, or one of the app's own: `client.new_tab`,
`client.split_right`, `client.zoom`, `client.palette`, `client.mode`, and
the rest `hive keymap` lists. `h.bind(key, fn)` binds a function.

You can change any key in the [settings](./settings#keys) file too, and
those win over scripts'. `hive keymap` shows every binding and where it
came from. The app takes keys only from the server on its own Mac; actions
of other hosts are in the command palette, under the host's name. On the
phone, every host's actions are in the switcher, under the host.

## Permissions

A script can only do what it was allowed. Without `--allow`, it can read:
list sessions, describe blocks, hear events.

| Permission | Allows |
| --- | --- |
| `read` | Sessions, windows, blocks, agents, the server's details, events. Every script has it. |
| `screen` | What terminals show: `format`, and the folders `list_dir` reads |
| `control` | Arrange, label, focus and close; new panes with your shell, in any folder |
| `spawn` | New panes that run a command. Includes `control`. |
| `input` | Typing into terminals: `write` and `send_key`, and answering agents |
| `ui` | Notifications, badges, actions and keys |
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

## Scripts that keep running

Put a script in the scripts folder, `~/.config/hive/scripts` (beside the
settings file, or `HIVE_SCRIPTS`), and the server runs it whenever it runs:
it starts after your sessions are restored, and keeps running with every
app closed. A script there is `NAME.ts` or `NAME/index.ts`. Shared code goes
in `lib/`, which isn't run on its own.

```ts
// ~/.config/hive/scripts/tests-on-done.ts
export default hive.script({
  name: "tests-on-done",
  permissions: ["spawn"],
  setup(h) {
    h.on("terminal.agent_changed", { state: "done" }, async (ctx, ev) => {
      const [tests] = await (await h.block(ev.block_id)).new_split({
        layout: { command: ["make", "test"], keep_open: true }, focus: false,
      });
      const { exit_code } = await tests.wait({ timeout: "10m" });
      console.log(exit_code === 0 ? "tests pass" : "tests fail");
    });
  },
});
```

A new script waits until you allow it:

```sh
hive script ls                 # tests-on-done  pending  asks spawn
hive script grant tests-on-done
```

You grant a script once. Editing it doesn't ask again; asking for more
permissions does.

| Command | Does |
| --- | --- |
| `hive script ls` | Every script in the folder: running, pending (waiting for a grant), stopped (it called `h.stop()`), failed, disabled or quarantined |
| `hive script grant NAME` / `revoke NAME` | Allow what it asks for, or take it back |
| `hive script disable NAME` / `enable NAME` | Turn it off or on, without removing it |
| `hive script reload [NAME]` | Load it again now |
| `hive script logs NAME [-f]` | Its log (`-f` keeps following) |
| `hive script audit [NAME]` | What it changed |

When you save a script, the server loads the new version within a few
seconds. If the new version has an error, the old one keeps running, and
`hive script ls` shows the error.

A script in the folder can keep values with `h.store`. They survive reloads
and restarts:

```ts
export default hive.script({
  name: "counter",
  permissions: [],
  async setup(h) {
    const starts = ((await h.store.get("starts")) ?? 0) + 1;
    await h.store.set("starts", starts);
    console.log("started " + starts + " times");
  },
});
```

`h.store.get`, `set`, `delete` and `keys(prefix)` take JSON values, up to
1 MB per script.

These events help scripts in the folder: `server_started`, then
`restore_finished` (with `sessions` and `agents_resumed`), and
`config_changed` (with the `keys` that changed).

### When a script misbehaves

- An error in a handler is logged, and the script keeps running.
- A handler that runs for more than 5 seconds without a break is stopped.
  Five of those in 5 minutes, and the script is quarantined: stopped until
  you `hive script enable` it, even if you edit it meanwhile.
- If the server's memory grows by more than 1 GB, the busiest script is
  quarantined.
- At most 64 scripts run at once. A script that starts another stops it
  when it stops.
- A script's own changes don't wake its own `h.on` handlers (pass
  `{ self: true }` as a fourth argument to hear them). Scripts that keep
  waking each other are stopped after 8 rounds: the call fails with
  `loop_suspected`.

Set `scripts = false` in the [settings](./settings) to start the server
without them.

## Safety

Scripts run as you, inside the server. Permissions protect you from a
script's mistakes, not from someone who can already reach your server: that
is what the Unix socket and your tailnet are for.

Everything a script changes (arranging, running, typing, settings) goes to
an audit log, `scripts/audit.jsonl` in the server's state folder. Text a
script types is logged as its length and a hash, not the text.

Imports must be your own files (`./lib`), not packages.
