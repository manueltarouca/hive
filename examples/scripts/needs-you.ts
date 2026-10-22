// needs-you: the apps already tell you when an agent stops. This reminds
// you when one has kept waiting, and gives you a key (⌃⌘J) that takes you
// to the agent that has waited longest.

const nameOf = (row: hive.AgentRow) => row.label || row.agent.cwd?.split("/").pop() || "An agent";
const minutes = (iso: string) => Math.max(1, Math.round((Date.now() - Date.parse(iso)) / 60_000));

/** Who waits for you, longest first: questions and prompts, then agents stopped by an error. */
export function waiting(rows: hive.AgentRow[], withDone: boolean): hive.AgentRow[] {
  const limit = (r: hive.AgentRow) => /\blimit\b/i.test(r.agent.message ?? "");
  const rank = (r: hive.AgentRow) =>
    r.agent.state === "blocked" ? 0 : r.agent.state === "done" && r.agent.kind === "error" ? (limit(r) ? 3 : 1) : r.agent.state === "done" ? 2 : 9;
  return rows
    // Stopped at a usage limit: waiting on the clock, not on you.
    .filter((r) => rank(r) < 2 || (withDone && rank(r) === 2))
    .sort((a, b) => rank(a) - rank(b) || Date.parse(a.agent.updated_at) - Date.parse(b.agent.updated_at));
}

export default hive.script({
  name: "needs-you",
  description: "Reminds you of agents that keep waiting, and ⌃⌘J takes you to the one waiting longest.",
  permissions: ["ui"],
  options: {
    remind_min: { title: "Remind me after this many minutes", type: "number", default: 10, min: 1, max: 240 },
    reminders: { title: "Reminders per wait (0: none)", type: "number", default: 2, min: 0, max: 10 },
    include_done: { title: "Also agents that finished", default: false },
  },
  setup(h) {
    const timers = new Map<string, { cancel(): void }>();

    const watch = (row: hive.AgentRow, told = 0) => {
      timers.get(row.block_id)?.cancel();
      if (told >= (h.options.reminders as number)) return;
      timers.set(row.block_id, h.after((h.options.remind_min as number) * 60_000, async () => {
        const now = (await h.agents.list()).find((r) => r.block_id === row.block_id);
        if (!now || now.agent.revision !== row.agent.revision) return;
        if (!waiting([now], h.options.include_done as boolean).length) return;
        await h.notify.send({
          title: `${nameOf(now)} has waited ${minutes(now.agent.updated_at)} minutes`,
          body: now.agent.message ?? "",
          target: now.block_id,
          level: "warn",
          actions: [{ title: "No more reminders", action: "hive.set_option", args: { key: "reminders", value: 0 },
            step: "needs-you stops reminding; the key still works" }],
        });
        watch(now, told + 1);
      }));
    };

    h.on("terminal.agent_changed", async (ctx, ev) => {
      timers.get(ev.block_id!)?.cancel();
      timers.delete(ev.block_id!);
      if (ev.state === "blocked" || ev.state === "done") {
        const row = (await h.agents.list()).find((r) => r.block_id === ev.block_id);
        if (row && waiting([row], h.options.include_done as boolean).length) watch(row);
      }
    });

    h.actions.register({
      name: "next-waiting",
      title: "Go to the agent waiting longest",
      keywords: ["agent", "blocked", "waiting", "next"],
      run: async (ctx) => {
        const next = waiting(await h.agents.list(), true)[0];
        if (!next) return "nobody is waiting";
        if (ctx.client_id) {
          await h.client.perform({ client_id: ctx.client_id, action: "client.reveal", args: { block_id: next.block_id } as any });
        }
        return `${nameOf(next)}: ${next.agent.state}${next.agent.message ? ", " + next.agent.message : ""}`;
      },
    });
    h.bind("ctrl+cmd+j", "next-waiting");

    // After a reload or a restart: agents that were already waiting.
    h.after(1000, async () => {
      for (const row of waiting(await h.agents.list(), h.options.include_done as boolean)) watch(row);
    });
  },
});
