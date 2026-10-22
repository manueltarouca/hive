// dev-panes: in shell panes (not agents), mark a failed test run and the
// port a dev server listens on. Each mark goes when the pane moves on.

const SHELLS = new Set(["zsh", "bash", "fish", "sh", "-zsh", "-bash", "nu"]);

export default hive.script({
  name: "dev-panes",
  description: "Marks failed test runs and dev server ports in shell panes.",
  permissions: ["screen", "ui"],
  options: {
    fails: { title: "Mark failed test runs", default: true },
    ports: { title: "Mark dev server ports", default: true },
  },
  setup(h) {
    const marks = new Map<string, { id: string; pid: number; name: string; at: number }>(); // block/id to the program that printed it
    const isAgent = async (block_id: string) => (await h.agents.list()).some((r) => r.block_id === block_id && r.agent.state !== "ended");
    const fg = async (block_id: string) => {
      const p: any = await h.terminal.process({ block_id });
      return p.foreground ?? { pid: p.child_pid, name: "" };
    };
    const mark = async (block_id: string, id: string, state: "error" | "done", msg: string) => {
      if (await isAgent(block_id)) return;
      const p = await fg(block_id);
      marks.set(block_id + "/" + id, { id, pid: p.pid, name: p.name, at: Date.now() });
      await h.status.set({ target: block_id, id, state, msg });
    };
    const clear = async (key: string) => {
      const m = marks.get(key);
      if (!m) return;
      marks.delete(key);
      await h.status.clear({ target: key.split("/")[0], id: m.id }).catch(() => {});
    };

    h.trigger({ pattern: "^\\s*(--- FAIL|FAIL\\b|panic:|npm ERR!|error\\[E\\d+\\]|Traceback \\(most recent call last\\)|\\d+ (failed|failing)\\b)", min_interval: "1s" },
      async (ctx, m) => { if (h.options.fails) await mark(m.block_id, "fail", "error", m.line.trim().slice(0, 80)); });
    h.trigger({ pattern: "(?i)(listening|local:|running (on|at)|serving|server started).*?:(\\d{2,5})\\b" },
      async (ctx, m) => { if (h.options.ports) await mark(m.block_id, "port", "done", `port ${m.groups[m.groups.length - 1]}`); });

    // A port mark lasts while its server runs; a failure until the next
    // command starts.
    h.every("5s", async () => {
      for (const [key, m] of [...marks]) {
        const block_id = key.split("/")[0];
        try {
          const now = await fg(block_id);
          // The line can come before the pane says which program runs: a
          // shell seen then is the server's, once it shows.
          if (SHELLS.has(m.name) && m.id === "port") {
            if (!SHELLS.has(now.name)) {
              Object.assign(m, { pid: now.pid, name: now.name });
            } else if (Date.now() - m.at > 8000) {
              await clear(key); // only the shell: a command line that mentioned a port, no server
            }
            continue;
          }
          if (m.id === "port" ? now.pid !== m.pid : now.pid !== m.pid && !SHELLS.has(now.name)) await clear(key);
        } catch {
          marks.delete(key);
        }
      }
    });
  },
});
