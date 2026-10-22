// digest: one notification that says where every agent is, from the
// palette, the phone or a key, and optionally each morning.

type Change = { t: number; label: string; session: string; state: string; kind?: string };

const nameOf = (row: hive.AgentRow) => row.label || row.agent.cwd?.split("/").pop() || "agent";
const ago = (iso: string) => {
  const m = Math.round((Date.now() - Date.parse(iso)) / 60_000);
  return m < 60 ? `${m}m` : `${Math.round(m / 60)}h`;
};
const plural = (n: number, one: string, many: string) => `${n} ${n === 1 ? one : many}`;

/** The digest's title and body. */
export function summarize(rows: hive.AgentRow[], changes: Change[], since: number): { title: string; body: string } {
  const live = rows.filter((r) => r.agent.state !== "ended");
  const is = (r: hive.AgentRow, s: string) =>
    s === "stopped" ? r.agent.state === "done" && r.agent.kind === "error" : r.agent.state === s && !(s === "done" && r.agent.kind === "error");
  const counts = [["blocked", "waiting"], ["stopped", "stopped"], ["working", "working"], ["done", "done"], ["idle", "idle"]]
    .map(([s, word]) => [live.filter((r) => is(r, s)).length, word] as const)
    .filter(([n]) => n > 0)
    .map(([n, word]) => `${n} ${word}`);
  const lines: string[] = [];
  const sessions = [...new Set(live.map((r) => r.session_label || r.session_id))].sort();
  for (const s of sessions) {
    const mine = live.filter((r) => (r.session_label || r.session_id) === s);
    const notable = mine
      .filter((r) => r.agent.state === "blocked" || r.agent.state === "done")
      .map((r) => `${nameOf(r)} ${r.agent.kind === "error" ? "stopped" : r.agent.state === "blocked" ? "waiting" : "done"} ${ago(r.agent.updated_at)}`);
    const working = mine.filter((r) => r.agent.state === "working").length;
    lines.push(`${s}: ${[...notable, working ? `${working} working` : ""].filter(Boolean).join(", ") || "all idle"}`);
  }
  const recent = changes.filter((c) => c.t > since);
  if (recent.length) {
    const limits = recent.filter((c) => c.kind === "error").length;
    lines.push(`Since the last digest: ${plural(recent.length, "change", "changes")}${limits ? `, ${plural(limits, "stop", "stops")} at a limit` : ""}.`);
  }
  return { title: counts.join(", ") || "No agents", body: lines.join("\n") };
}

export default hive.script({
  name: "digest",
  description: "One notification with where every agent is: from the palette or the phone, and each morning if you like.",
  permissions: ["ui"],
  options: {
    morning: { title: "Send it each day at (HH:MM, empty: never)", default: "" },
  },
  async setup(h) {
    let changes: Change[] = (await h.store.get<Change[]>("changes")) ?? [];
    let last: number = (await h.store.get<number>("last")) ?? 0;

    h.on("terminal.agent_changed", async (ctx, ev) => {
      if (ev.state === "working") return; // the rest is what you'd want to know
      const row = (await h.agents.list()).find((r) => r.block_id === ev.block_id);
      changes.push({ t: Date.now(), label: row ? nameOf(row) : "agent", session: row?.session_label ?? "", state: ev.state, kind: ev.kind });
      changes = changes.filter((c) => c.t > Date.now() - 2 * 86_400_000).slice(-400);
      await h.store.set("changes", changes);
    });

    const send = async () => {
      const d = summarize(await h.agents.list(), changes, last);
      last = Date.now();
      await h.store.set("last", last);
      await h.notify.send({ title: d.title, body: d.body });
      return d.title;
    };
    h.actions.register({ name: "digest", title: "Agents digest", keywords: ["status", "agents", "summary"], run: () => send() });

    h.every("1m", async () => {
      const at = String(h.options.morning || "").trim();
      if (!/^\d{1,2}:\d{2}$/.test(at)) return;
      const now = new Date();
      const today = now.toDateString();
      if (now.toTimeString().slice(0, 5) === at.padStart(5, "0") && (await h.store.get("sent_on")) !== today) {
        await h.store.set("sent_on", today);
        await send();
      }
    });
  },
});
