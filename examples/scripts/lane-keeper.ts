// lane-keeper: for a team of agents in one session with an orchestrator
// (one agent that hands out work), notice a lane that went quiet without
// asking anything, and tell the orchestrator. By default it only marks the
// lane and asks you; telling the orchestrator by itself is a switch.

const nameOf = (row: hive.AgentRow) => row.label || row.agent.cwd?.split("/").pop() || "a lane";

export default hive.script({
  name: "lane-keeper",
  description: "Notices team lanes that went idle without asking anything, and tells the orchestrator.",
  permissions: ["input", "ui"],
  options: {
    session: { title: "The team's session", default: "opensaga" },
    orchestrator: { title: "The orchestrator's pane label", default: "orchestrator" },
    minutes: { title: "Idle for this many minutes", type: "number", default: 10, min: 2, max: 240 },
    auto_tell: { title: "Tell the orchestrator by itself (off: ask me first)", default: false },
  },
  setup(h) {
    const timers = new Map<string, { cancel(): void }>();
    const queued = new Map<string, string>(); // lane block to message, waiting for the orchestrator

    const inTeam = (r: hive.AgentRow) => (r.session_label ?? "") === h.options.session;
    const isOrchestrator = (r: hive.AgentRow) => (r.label ?? "") === h.options.orchestrator;

    const deliver = async () => {
      if (queued.size === 0) return;
      const rows = await h.agents.list();
      const orch = rows.find((r) => inTeam(r) && isOrchestrator(r));
      if (!orch || !["idle", "done"].includes(orch.agent.state)) return; // when it stops, it hears
      const text = `[hive lane-keeper] ${[...queued.values()].join(" ")} Give each its next unit, or tell it to hand over.`;
      try {
        await h.agent.act({ block_id: orch.block_id, revision: orch.agent.revision, expect_state: ["idle", "done"], write: text + "\r" });
        await h.notify.send({
          title: `Told the orchestrator about ${plural(queued.size)}`,
          body: [...queued.values()].join("\n"),
          target: orch.block_id,
          actions: [{ title: "Ask me first", action: "hive.set_option", args: { key: "auto_tell", value: false } }],
        });
        queued.clear();
      } catch (e: any) {
        if (e.code !== "conflict") console.warn("couldn't tell the orchestrator: " + e.message);
      }
    };
    const plural = (n: number) => (n === 1 ? "1 idle lane" : `${n} idle lanes`);

    const check = async (lane: hive.AgentRow) => {
      const now = (await h.agents.list()).find((r) => r.block_id === lane.block_id);
      if (!now || now.agent.revision !== lane.agent.revision || !["idle", "done"].includes(now.agent.state)) return;
      if (now.agent.kind === "error") return; // stopped at a limit: limit-resume's job
      const mins = Math.round((Date.now() - Date.parse(now.agent.updated_at)) / 60_000);
      await h.status.set({ target: now.block_id, id: "idle", state: "idle", msg: `idle ${mins}m` });
      const line = `${nameOf(now)} has been idle ${mins} min${now.agent.message ? ` (last: ${now.agent.message.slice(0, 120)})` : ""}.`;
      if (h.options.auto_tell) {
        queued.set(now.block_id, line);
        await deliver();
      } else {
        await h.notify.send({
          title: `${nameOf(now)} has been idle ${mins} minutes`,
          body: now.agent.message ?? "It stopped without asking anything.",
          target: now.block_id,
          actions: [
            { title: "Tell the orchestrator", action: "tell", args: { block_id: now.block_id, line } },
            { title: "Always tell it", action: "hive.set_option", args: { key: "auto_tell", value: true } },
          ],
        });
      }
    };

    h.actions.register<{ block_id: string; line: string }>({
      name: "tell",
      title: "Tell the orchestrator about an idle lane",
      run: async (ctx, a) => {
        queued.set(a.block_id, a.line);
        await deliver();
        return queued.size ? "the orchestrator is busy: it hears when it stops" : "told";
      },
    });

    // After a reload or a restart: lanes already idle.
    h.after(1000, async () => {
      for (const row of await h.agents.list()) {
        if (!inTeam(row) || isOrchestrator(row) || !["idle", "done"].includes(row.agent.state)) continue;
        const left = (h.options.minutes as number) * 60_000 - (Date.now() - Date.parse(row.agent.updated_at));
        timers.set(row.block_id, h.after(Math.max(1000, left), () => check(row)));
      }
    });

    h.on("terminal.agent_changed", async (ctx, ev) => {
      timers.get(ev.block_id!)?.cancel();
      const row = (await h.agents.list()).find((r) => r.block_id === ev.block_id);
      if (!row || !inTeam(row)) return;
      if (isOrchestrator(row)) return deliver();
      if (ev.state === "idle" || ev.state === "done") {
        timers.set(row.block_id, h.after((h.options.minutes as number) * 60_000, () => check(row)));
      } else {
        queued.delete(row.block_id);
        await h.status.clear({ target: row.block_id, id: "idle" }).catch(() => {});
      }
    });
  },
});
