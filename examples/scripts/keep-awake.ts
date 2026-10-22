// keep-awake: while any agent works, keep the Mac from sleeping with a
// "caffeinate" tab (yours, if you have one). It closes only a tab it
// opened, after the agents have been idle for a while.

export default hive.script({
  name: "keep-awake",
  description: "Keeps the Mac awake with a caffeinate tab while agents work.",
  permissions: ["spawn", "ui"],
  options: {
    display: { title: "Keep the display on too (stops the screen locking)", default: true },
    idle_min: { title: "Close it after this many idle minutes", type: "number", default: 30, min: 5, max: 600 },
    session: { title: "Open it in this session (empty: the first)", default: "" },
  },
  async setup(h) {
    const info = await h.server.version();
    if (!String(info.platform).startsWith("darwin")) {
      console.log("not a Mac: nothing to do");
      return h.stop();
    }
    let idleSince = 0;

    // A pane really running caffeinate. After a restart a pane comes back
    // as a shell, so a tab's label alone proves nothing.
    const find = async (): Promise<{ running?: string; dead: string[] }> => {
      const dead: string[] = [];
      for (const s of Object.values<any>((await h.session.list()).sessions ?? [])) {
        for (const b of ((await h.session.list_blocks({ session_id: s.session_id })) as any).blocks ?? []) {
          const p: any = await h.terminal.process({ block_id: b.block_id }).catch(() => ({}));
          if (p.foreground?.program === "caffeinate") return { running: b.block_id, dead };
          if (b.label === "caffeinate") dead.push(b.block_id);
        }
      }
      return { dead };
    };

    h.every("1m", async () => {
      const working = (await h.agents.list()).some((r) => r.agent.state === "working");
      const mine: string | undefined = await h.store.get("opened");
      const { running: have, dead } = await find();
      if (working) {
        idleSince = 0;
        if (have) return;
        // Our own tab, back as a shell after a restart: replace it.
        if (mine && dead.includes(mine)) await (await h.block(mine)).close().catch(() => {});
        const sessions: any[] = Object.values((await h.session.list()).sessions ?? []);
        const target = sessions.find((s: any) => s.label === h.options.session) ?? sessions[0];
        if (!target) return;
        const flags = h.options.display ? "-dims" : "-is";
        const [b] = await (await h.session_of(target.session_id)).new_window({
          label: "caffeinate", focus: false, layout: { command: ["caffeinate", flags] } as any,
        });
        // The pane's own label: find() reads it back after a restart.
        await h.session.set_block_label({ session_id: target.session_id, block_id: b.id, label: "caffeinate" });
        await h.store.set("opened", b.id);
        await h.notify.send({
          title: "Keeping the Mac awake",
          body: `Agents are working: opened a caffeinate tab (caffeinate ${flags}) in ${target.label}.` +
            (dead.some((d) => d !== mine) ? " A caffeinate tab you have shows a shell, not caffeinate." : ""),
          target: b.id,
          actions: [{ title: "Stop doing this", action: "hive.set_mode", args: { mode: "watch" } }],
        });
        return;
      }
      if (!idleSince) idleSince = Date.now();
      if (mine && have === mine && Date.now() - idleSince > (h.options.idle_min as number) * 60_000) {
        await (await h.block(mine)).close().catch(() => {});
        await h.store.delete("opened");
        await h.notify.send({ title: "Closed the caffeinate tab", body: `No agent has worked for ${h.options.idle_min} minutes.` });
      }
    });
  },
});
