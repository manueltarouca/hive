// limit-resume: when an agent stops at its usage limit, mark its pane with
// when the limit resets, and type "resume" into it then. Every resume is a
// notification, with buttons to stop doing it.

const MONTHS = ["jan", "feb", "mar", "apr", "may", "jun", "jul", "aug", "sep", "oct", "nov", "dec"];

/** When a limit message says it resets, as a time; null when it doesn't say. */
export function parseReset(msg: string, now: Date): Date | null {
  const s = msg.toLowerCase();
  const rel = s.match(/resets?\s+in\s+(?:(\d+)\s*h(?:ours?)?)?\s*(?:(\d+)\s*m(?:in(?:utes?)?)?)?/);
  if (rel && (rel[1] || rel[2])) {
    return new Date(now.getTime() + ((+(rel[1] ?? 0)) * 60 + +(rel[2] ?? 0)) * 60_000);
  }
  const at = s.match(/resets?\s+(?:at\s+|on\s+)?(?:([a-z]{3})[a-z]*\s+(\d{1,2}),?\s+(?:at\s+)?)?(\d{1,2})(?::(\d{2}))?\s*(am|pm)?/);
  if (!at) return null;
  let hour = +at[3];
  const minute = +(at[4] ?? 0);
  if (at[5] === "pm" && hour < 12) hour += 12;
  if (at[5] === "am" && hour === 12) hour = 0;
  if (!at[5] && at[4] === undefined) return null; // "resets 3" says nothing
  if (hour > 23 || minute > 59) return null;
  const t = new Date(now.getTime());
  t.setSeconds(0, 0);
  t.setHours(hour, minute);
  if (at[1]) {
    const month = MONTHS.indexOf(at[1]);
    if (month < 0) return null;
    t.setMonth(month, +at[2]);
    if (t.getTime() < now.getTime() - 183 * 86_400_000) t.setFullYear(t.getFullYear() + 1);
  } else if (t.getTime() <= now.getTime()) {
    t.setDate(t.getDate() + 1);
  }
  return t;
}

const LIMIT = /\blimit\b/i;
const HOUR = 3_600_000;

type Pending = { block_id: string; label: string; resets_at: number | null; from_screen: boolean; told?: boolean };

const hhmm = (t: number) => new Date(t).toTimeString().slice(0, 5);

export default hive.script({
  name: "limit-resume",
  description: "When an agent stops at its usage limit, types resume into it once the limit resets.",
  // screen: to read the limit off the screen for agents that don't report it.
  permissions: ["input", "screen", "ui"],
  options: {
    auto: { title: "Type it by itself (off: only tell me, with a button)", default: true },
    text: { title: "What to type", default: "resume" },
    delay_min: { title: "Minutes to wait after the reset", type: "number", default: 2, min: 0, max: 60 },
  },
  async setup(h) {
    const pending = new Map<string, Pending>((await h.store.get<[string, Pending][]>("pending")) ?? []);
    let timer: { cancel(): void } | undefined;
    const save = () => h.store.set("pending", [...pending]);
    const nameOf = (row: hive.AgentRow) => row.label || row.agent.cwd?.split("/").pop() || "an agent";

    const badge = async (p: Pending) => {
      const msg = p.resets_at ? `limited until ${hhmm(p.resets_at)}` : "limited, reset time unknown";
      await h.status.set({ target: p.block_id, id: "limit", state: "error", msg }).catch(() => {});
    };
    const forget = async (block_id: string) => {
      if (!pending.delete(block_id)) return;
      await save();
      await h.status.clear({ target: block_id, id: "limit" }).catch(() => {});
      schedule();
    };
    // One timer, for the earliest reset; a time we couldn't read is
    // looked at every hour.
    const schedule = () => {
      timer?.cancel();
      if (pending.size === 0) return;
      const delay = ((h.options.delay_min as number) ?? 2) * 60_000;
      const next = Math.min(...[...pending.values()].map((p) => (p.resets_at ?? Date.now() + HOUR) + delay));
      timer = h.after(Math.max(1000, next - Date.now()), fire);
    };
    const arm = async (row: hive.AgentRow, text: string, from_screen: boolean) => {
      if (pending.has(row.block_id)) return;
      const p = { block_id: row.block_id, label: nameOf(row), resets_at: parseReset(text, new Date())?.getTime() ?? null, from_screen };
      pending.set(row.block_id, p);
      await save();
      await badge(p);
      console.log(`${p.label} is at its limit; ${p.resets_at ? "resets " + hhmm(p.resets_at) : "reset time unknown"}`);
      schedule();
    };

    const fire = async () => {
      const delay = ((h.options.delay_min as number) ?? 2) * 60_000;
      const due = [...pending.values()].filter((p) => p.resets_at === null || p.resets_at + delay <= Date.now());
      const rows = await h.agents.list();
      const resumed: string[] = [];
      const waiting: Pending[] = [];
      for (const p of due) {
        const row = rows.find((r) => r.block_id === p.block_id);
        // Read off the screen, the agent may still say it's working (its hook
        // never ran); the banner, checked again when typing, decides.
        const states = p.from_screen ? ["done", "idle", "working"] : ["done", "idle"];
        if (!row || !states.includes(row.agent.state)) {
          await forget(p.block_id); // it went on, or someone resumed it
          continue;
        }
        if (!h.options.auto) {
          waiting.push(p);
          continue;
        }
        try {
          await h.agent.act({
            block_id: p.block_id, revision: row.agent.revision, expect_state: states as any,
            write: String(h.options.text || "resume") + "\r",
            // Read off the screen: only if the screen still says so.
            ...(p.from_screen ? { confirm: "(You've hit your \\w+ limit|Usage limit reached)" } : {}),
          });
          resumed.push(p.label);
          await forget(p.block_id);
        } catch (e: any) {
          if (e.code === "conflict") await forget(p.block_id);
          else console.warn(`couldn't resume ${p.label}: ${e.message}`);
        }
      }
      if (resumed.length > 0) {
        await h.notify.send({
          title: resumed.length === 1 ? `Resumed ${resumed[0]}` : `Resumed ${resumed.length} agents`,
          body: `Its usage limit reset; typed "${h.options.text}". ${resumed.length > 1 ? resumed.join(", ") : ""}`.trim(),
          actions: [
            { title: "Stop typing it", action: "hive.set_option", args: { key: "auto", value: false }, step: "limit-resume will only tell you next time" },
            { title: "Watch only", action: "hive.set_mode", args: { mode: "watch" }, step: "limit-resume may mark panes but not type" },
          ],
        });
      }
      // Typing by itself is off: one notification for all of them, once.
      const untold = waiting.filter((p) => !p.told);
      if (untold.length) {
        await h.notify.send({
          title: untold.length === 1 ? `${untold[0].label}'s limit has reset` : `${untold.length} agents' limits have reset`,
          body: "limit-resume didn't type anything: typing by itself is off.\n" + untold.map((p) => p.label).join(", "),
          target: untold.length === 1 ? untold[0].block_id : undefined,
          actions: [
            ...(untold.length === 1 ? [{ title: "Resume it", action: "resume", args: { block_id: untold[0].block_id } }] : []),
            { title: "Resume all", action: "resume-all" },
            { title: "Type by itself", action: "hive.set_option", args: { key: "auto", value: true } },
          ],
        });
      }
      for (const p of waiting) {
        p.told = true;
        p.resets_at = null; // looked at again in an hour, quietly
      }
      if (waiting.length) await save();
      schedule();
    };

    h.actions.register<{ block_id: string }>({
      name: "resume",
      title: "Resume an agent stopped at its limit",
      run: async (ctx, a) => {
        const row = (await h.agents.list()).find((r) => r.block_id === a.block_id);
        if (!row) return "it's gone";
        await h.agent.act({ block_id: a.block_id, revision: row.agent.revision, expect_state: ["done", "idle"],
          write: String(h.options.text || "resume") + "\r" });
        await forget(a.block_id);
        return "resumed";
      },
    });

    h.actions.register({
      name: "resume-all",
      title: "Resume every agent stopped at its limit",
      run: async () => {
        const rows = await h.agents.list();
        const done: string[] = [];
        for (const p of [...pending.values()]) {
          const row = rows.find((r) => r.block_id === p.block_id);
          if (!row) { await forget(p.block_id); continue; }
          try {
            await h.agent.act({ block_id: p.block_id, revision: row.agent.revision, expect_state: ["done", "idle"],
              write: String(h.options.text || "resume") + "\r" });
            done.push(p.label);
          } catch {}
          await forget(p.block_id);
        }
        return done.length ? "resumed " + done.join(", ") : "nothing to resume";
      },
    });

    // The agent's own report: done, kind error, with the reason.
    h.on("terminal.agent_changed", async (ctx, ev) => {
      if (ev.state === "working" || ev.state === "ended") return forget(ev.block_id!);
      if (ev.state === "done" && ev.kind === "error" && LIMIT.test(ev.message ?? "")) {
        const row = (await h.agents.list()).find((r) => r.block_id === ev.block_id);
        if (row) await arm(row, row.agent.message ?? "", false);
      }
    });
    // Or the screen, for agents that don't report it: Claude Code's own
    // banner, at the start of a line (not an agent quoting it).
    h.trigger({ pattern: "^[\\s⎿│]*(You've hit your \\w+ limit|Usage limit reached) · (limit )?resets", min_interval: "30s" }, async (ctx, m) => {
      const row = (await h.agents.list()).find((r) => r.block_id === m.block_id);
      if (row && row.agent.state !== "ended") await arm(row, m.line, true);
    });

    // After a reload or a restart: what was pending, and agents stopped now.
    for (const row of await h.agents.list()) {
      if (row.agent.state === "done" && row.agent.kind === "error" && LIMIT.test(row.agent.message ?? "")) {
        await arm(row, row.agent.message ?? "", false);
      }
    }
    for (const p of pending.values()) await badge(p);
    schedule();
    h.on("options_changed", () => schedule());
  },
});
