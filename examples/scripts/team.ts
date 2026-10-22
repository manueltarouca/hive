// team: do one thing to every agent in a session at once: pause them,
// resume the ones you paused, reload their skills, or say one line to all.
// Each says in a notification what it did, and to whom.

const nameOf = (row: hive.AgentRow) => row.label || row.agent.cwd?.split("/").pop() || "agent";

export default hive.script({
  name: "team",
  description: "Pause, resume, reload skills or say one line to every agent in a session.",
  permissions: ["input", "ui"],
  options: {
    resume_text: { title: "What to type to resume", default: "continue" },
  },
  setup(h) {
    const agentsIn = async (session_id?: string) =>
      (await h.agents.list()).filter((r) => r.agent.state !== "ended" && (!session_id || r.session_id === session_id));
    // Every session only when asked: from the phone or the CLI there may be
    // no session at hand, and that must not mean all of them.
    const where = (ctx: hive.Context, a: { all?: boolean } | undefined) => {
      if (ctx.session_id) return ctx.session_id;
      if (a?.all) return undefined;
      throw new Error("run it from a pane, or pass all=true for every session");
    };

    // Type into each agent that is in one of these states; say who got it.
    const each = async (rows: hive.AgentRow[], states: string[], act: Record<string, unknown>) => {
      const done: hive.AgentRow[] = [];
      for (const r of rows.filter((r) => states.includes(r.agent.state))) {
        try {
          await h.agent.act({ block_id: r.block_id, revision: r.agent.revision, expect_state: states as any, ...act } as any);
          done.push(r);
        } catch (e: any) {
          if (e.code !== "conflict") console.warn(`${nameOf(r)}: ${e.message}`);
        }
      }
      return done;
    };
    const tell = async (what: string, rows: hive.AgentRow[], skipped: number) => {
      await h.notify.send({
        title: rows.length ? `${what} ${rows.length === 1 ? nameOf(rows[0]) : rows.length + " agents"}` : `${what} nobody`,
        body: [rows.map(nameOf).join(", "), skipped ? `${skipped} skipped: busy or waiting on you` : ""].filter(Boolean).join("\n"),
      });
      return rows.map(nameOf);
    };

    h.actions.register({
      name: "pause",
      title: "Pause this session's agents",
      keywords: ["stop", "halt", "interrupt", "agents"],
      run: async (ctx, a: any) => {
        const rows = await agentsIn(where(ctx, a));
        const paused = await each(rows, ["working"], { send_key: ["escape"] });
        await h.store.set("paused", paused.map((r) => r.block_id));
        return tell("Paused", paused, rows.length - paused.length);
      },
    });
    h.actions.register({
      name: "resume",
      title: "Resume the agents you paused",
      keywords: ["continue", "agents"],
      run: async (ctx) => {
        const ids: string[] = (await h.store.get("paused")) ?? [];
        const rows = (await agentsIn()).filter((r) => ids.includes(r.block_id));
        const resumed = await each(rows, ["idle", "done"], { write: String(h.options.resume_text || "continue") + "\r" });
        await h.store.set("paused", []);
        return tell("Resumed", resumed, rows.length - resumed.length);
      },
    });
    h.actions.register({
      name: "reload-skills",
      title: "Reload skills in this session's agents",
      keywords: ["skills", "agents"],
      run: async (ctx, a: any) => {
        const rows = (await agentsIn(where(ctx, a))).filter((r) => r.agent.harness === "claude");
        return tell("Reloaded skills in", await each(rows, ["idle", "done"], { write: "/reload-skills\r" }), 0);
      },
    });
    h.actions.register<{ text: string; all?: boolean }>({
      name: "broadcast",
      title: "Say one line to this session's agents",
      run: async (ctx, a) => {
        if (!a?.text) throw new Error('give it text: hive do broadcast text="..."');
        const rows = await agentsIn(where(ctx, a));
        const told = await each(rows, ["idle", "done"], { write: a.text + "\r" });
        return tell("Told", told, rows.length - told.length);
      },
    });
  },
});
