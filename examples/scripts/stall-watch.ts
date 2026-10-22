// stall-watch: an agent that says it's working while its screen hasn't
// moved for a while gets a "quiet" mark, and you hear about it once.
// Spinners, timers and the input box don't count as moving.

/** The part of an agent's screen that only changes when it does something. */
export function settled(text: string): string {
  const lines = text.split("\n");
  return lines
    .slice(0, Math.max(0, lines.length - 8)) // the input box and the status line below it
    .filter((l) => !/\(\d+m?\s?\d*s\b|\d+s\s·|esc to interrupt|tokens?\b|[✻✶✳✢·✽*]\s+\w+…/.test(l))
    .join("\n");
}

function hash(s: string): string {
  let a = 5381;
  for (let i = 0; i < s.length; i++) a = ((a << 5) + a + s.charCodeAt(i)) | 0;
  return String(a);
}

export default hive.script({
  name: "stall-watch",
  description: "Marks agents that say they're working but whose screen hasn't moved for 15 minutes.",
  permissions: ["screen", "ui"],
  options: {
    minutes: { title: "Quiet for this many minutes", type: "number", default: 15, min: 3, max: 240 },
    notify: { title: "Tell me, not only mark the pane", default: true },
  },
  setup(h) {
    const seen = new Map<string, { hash: string; since: number; marked: boolean }>();

    h.every("1m", async () => {
      const working = (await h.agents.list()).filter((r) => r.agent.state === "working");
      const ids = new Set(working.map((r) => r.block_id));
      for (const [id, s] of seen) {
        if (!ids.has(id)) {
          if (s.marked) await h.status.clear({ target: id, id: "quiet" }).catch(() => {});
          seen.delete(id);
        }
      }
      for (const row of working) {
        let text: string;
        try {
          text = await (await h.block(row.block_id)).format({ tail: 60 });
        } catch {
          continue;
        }
        const now = hash(settled(text));
        const s = seen.get(row.block_id);
        if (!s || s.hash !== now) {
          if (s?.marked) await h.status.clear({ target: row.block_id, id: "quiet" }).catch(() => {});
          seen.set(row.block_id, { hash: now, since: Date.now(), marked: false });
          continue;
        }
        const quiet = Math.round((Date.now() - s.since) / 60_000);
        if (quiet < (h.options.minutes as number)) continue;
        await h.status.set({ target: row.block_id, id: "quiet", state: "blocked", msg: `quiet ${quiet}m` });
        if (!s.marked && h.options.notify) {
          const name = row.label || row.agent.cwd?.split("/").pop() || "An agent";
          await h.notify.send({
            title: `${name} has shown nothing new for ${quiet} minutes`,
            body: "It says it's working. A long test run looks like this too.",
            target: row.block_id,
            actions: [{ title: "Only mark the pane", action: "hive.set_option", args: { key: "notify", value: false } }],
          });
        }
        s.marked = true;
      }
    });
  },
});
