// The scripting API, built on a few native functions in __host. Runs once
// per runtime, before the script. Names follow the wire: snake_case.
(function () {
  "use strict";
  const host = __host;
  delete globalThis.__host;

  class HiveError extends Error {
    constructor(e) {
      super(e.message);
      this.name = "HiveError";
      this.status = e.status;
      this.code = e.code;
      if (e.details !== undefined) this.details = e.details;
    }
  }

  // Durations: a number of milliseconds, or "250ms", "5s", "10m", "1h".
  function ms(d) {
    if (typeof d === "number") return d;
    const m = /^(\d+(?:\.\d+)?)(ms|s|m|h)$/.exec(String(d));
    if (!m) throw new HiveError({ status: 400, code: "bad_request", message: "a duration such as 500ms, 5s or 10m, not " + d });
    return Number(m[1]) * { ms: 1, s: 1000, m: 60000, h: 3600000 }[m[2]];
  }

  function call(method, args) {
    return host.call(method, JSON.stringify(args === undefined ? {} : args)).then(
      (out) => (out === "" ? undefined : JSON.parse(out)),
      (e) => { throw new HiveError(JSON.parse(e)); });
  }

  function log(level, args) {
    host.log(level, args.map((a) => {
      if (typeof a === "string") return a;
      if (a instanceof Error) return a.stack || String(a);
      try { return JSON.stringify(a); } catch (_) { return String(a); }
    }).join(" "));
  }

  // A handler's error is logged; the script keeps running.
  function guard(fn, ...args) {
    try {
      const v = fn(...args);
      if (v && typeof v.then === "function") v.then(undefined, (e) => log("error", [e]));
    } catch (e) {
      log("error", [e]);
    }
  }
  function ctxFor(origin, meta) {
    return Object.freeze(Object.assign({}, ctx, { origin }, meta ? { event: meta.event, seq: meta.seq, depth: meta.depth } : {}));
  }

  function after(d, fn) {
    const id = host.timer(ms(d), false, () => guard(fn, ctxFor("timer")));
    return { cancel: () => host.cancel(id), active: () => host.active(id) };
  }
  function every(d, fn) {
    const id = host.timer(ms(d), true, () => guard(fn, ctxFor("timer")));
    return { cancel: () => host.cancel(id), active: () => host.active(id) };
  }
  function sleep(d) { return new Promise((res) => host.timer(ms(d), false, res)); }

  function on(event, a, b, opts) {
    if (typeof a === "function" && b && typeof b === "object") { opts = b; b = undefined; }
    let filter = {}, fn = a;
    if (typeof a !== "function") { filter = a || {}; fn = b; }
    let jsFilter = null;
    if (typeof filter === "function") { jsFilter = filter; filter = {}; }
    const id = host.subscribe(String(event), JSON.stringify(filter), (ev, meta) => {
      const c = ctxFor("event", meta);
      if (jsFilter && !jsFilter(ev, c)) return;
      guard(fn, c, ev);
    }, !!(opts && opts.self));
    return { cancel: () => host.unsubscribe(id) };
  }
  function wait(event, filter, opts) {
    const timeout = opts && opts.timeout !== undefined ? ms(opts.timeout) : 0;
    return new Promise((resolve, reject) => {
      let timer = 0;
      const id = host.subscribe(String(event), JSON.stringify(typeof filter === "object" && filter ? filter : {}), (ev, meta) => {
        if (typeof filter === "function" && !filter(ev, ctxFor("event", meta))) return;
        host.unsubscribe(id);
        if (timer) host.cancel(timer);
        resolve(ev);
      }, true);
      if (timeout > 0) timer = host.timer(timeout, false, () => {
        host.unsubscribe(id);
        reject(new HiveError({ status: 408, code: "timeout", message: "no " + event + " within " + opts.timeout }));
      });
    });
  }

  // Output triggers: fn(ctx, match) for each line a terminal prints that
  // matches pattern (RE2).
  function triggerSpec(t) {
    const spec = Object.assign({}, t);
    if (spec.min_interval !== undefined) spec.min_interval_ms = ms(spec.min_interval);
    delete spec.min_interval;
    delete spec.timeout;
    return JSON.stringify(spec);
  }
  function trigger(t, fn) {
    const id = host.trigger(triggerSpec(t), (m) => guard(fn, ctxFor("trigger"), m));
    return { cancel: () => host.untrigger(id) };
  }
  function wait_output(t) {
    const timeout = t && t.timeout !== undefined ? ms(t.timeout) : 0;
    return new Promise((resolve, reject) => {
      let timer = 0;
      const id = host.trigger(triggerSpec(Object.assign({}, t, { once: true })), (m) => {
        if (timer) host.cancel(timer);
        resolve(m);
      });
      if (timeout > 0) timer = host.timer(timeout, false, () => {
        host.untrigger(id);
        reject(new HiveError({ status: 408, code: "timeout", message: "no output matching " + t.pattern + " within " + t.timeout }));
      });
    });
  }

  // Handles: thin wrappers over IDs. Every read is a call.
  function block(id) {
    const t = (name, args) => call("hive.terminal." + name, { block_id: id, args: args || {} });
    return {
      id,
      new_split: (o) => call("session.new_split", Object.assign({ block_id: id }, o || {})).then(
        (r) => r.block_ids.map(block)),
      format: (o) => t("format", o).then((r) => r.content),
      write: (text) => t("write", { data: text }).then(() => undefined),
      send_key: (...keys) => t("send_key", { keys }).then(() => undefined),
      title: () => t("title").then((r) => r.title),
      // The folder the program reported (OSC 7), else its working directory.
      pwd: () => t("pwd").then((r) => r.pwd || t("process").then((p) => (p.foreground && p.foreground.cwd) || "")),
      process: () => t("process"),
      wait: (o) => waitExit(id, o),
      label: (l) => l === undefined
        ? call("session.describe_block", { block_id: id }).then((r) => r.label || "")
        : call("session.set_block_label", { block_id: id, label: l }).then(() => l),
      zoom: (on) => call("session.zoom_block", on === undefined ? { block_id: id } : { block_id: id, zoom: on }).then(() => undefined),
      focus: () => call("session.focus_block", { block_id: id }).then(() => undefined),
      close: () => call("block.close", { block_id: id }).then(() => undefined),
      agent: () => call("agent.list", {}).then((r) => (r.agents || []).find((a) => a.block_id === id)),
    };
  }
  // Wait for a block's program to exit, with no 60 s cap: listen for the
  // event, and ask the server once in case it already exited.
  function waitExit(id, o) {
    const ev = wait("terminal.child_exited", { block_id: id }, o).then((e) => ({ exit_code: e.exit_code }));
    const now = call("hive.terminal.wait", { block_id: id, args: { timeout: 0.001 } }).then(
      (r) => (r && r.exited ? { exit_code: r.exit_code } : ev), () => ev);
    return Promise.race([now, ev]);
  }
  function session(id) {
    return {
      id,
      label: () => call("session.view", { session_id: id }).then((r) => r.label || ""),
      view: () => call("session.view", { session_id: id }),
      new_window: (o) => call("session.new_window", Object.assign({ session_id: id }, o || {})).then(
        (r) => r.block_ids.map(block)),
      close: () => call("session.close", { session_id: id }).then(() => undefined),
    };
  }

  let keyFns = 0;
  function errText(e) { return e instanceof Error ? (e.stack || String(e)) : String(e); }

  // ---- actions ----
  const actions = {
    register(spec) {
      if (!spec || typeof spec.run !== "function") throw new TypeError("an action needs a name and a run function");
      const info = Object.assign({}, spec);
      delete info.run;
      host.registerAction(JSON.stringify(info), (args, meta, ok, fail) => {
        const c = Object.freeze(Object.assign({}, ctx, { origin: meta.origin || "api" },
          meta.block_id ? { block_id: meta.block_id } : {}, meta.session_id ? { session_id: meta.session_id } : {},
          meta.client_id ? { client_id: meta.client_id } : {}));
        Promise.resolve().then(() => spec.run(c, args)).then(
          (v) => ok(v === undefined ? "" : JSON.stringify(v)),
          (e) => fail(errText(e), (e && e.code) || "error"));
      });
    },
    list: () => call("action.list", {}).then((r) => r.actions),
    run: (name, args) => call("action.run", { name, args: args || {} }),
  };

  // ---- agents ----
  function on_state(state, fn) {
    const states = Array.isArray(state) ? state : [state];
    return on("terminal.agent_changed", { state: states }, async (c, ev) => {
      const row = (await h.agents.list()).find((a) => a.block_id === ev.block_id);
      if (row) await fn(c, row);
    });
  }

  function stepText(d) {
    if (d.send_key) return "Press " + d.send_key.join(", then ") + " in the agent's pane";
    return "Type " + JSON.stringify(d.write) + " in the agent's pane";
  }

  // A rule watches agents and, when one is blocked on what it describes,
  // sends a notification whose button answers it. The button acts only if
  // the agent is still blocked on the same thing.
  function rule(r) {
    if (!r || !/^[a-z0-9][a-z0-9_-]*$/.test(r.name || "")) throw new TypeError("a rule needs a name: lowercase letters, digits, - and _");
    if ((r.mode || "suggest") !== "suggest") throw new TypeError("rules only suggest for now: mode \"act\" isn't available yet");
    const d = r.do || {};
    if (!Array.isArray(d.send_key) && typeof d.write !== "string") throw new TypeError("a rule's do needs send_key: [...] or write: \"...\"");
    const when = r.when || {};
    const filter = { state: when.state || "blocked" };
    // A question (elicitation_dialog) is blocked too: a rule only answers
    // the kind it names, permission prompts unless it says otherwise.
    if (filter.state === "blocked") filter.kind = when.kind || "permission_prompt";
    else if (when.kind) filter.kind = when.kind;
    if (when.harness) filter.harness = when.harness;
    if (when.message) host.re2(when.message, ""); // check the pattern now
    const cwd = when.cwd ? when.cwd.replace(/^~(?=\/|$)/, host.home()) : "";
    const action = "rule:" + r.name;
    actions.register({
      name: action, title: r.title || "Answer: " + r.name,
      run: (c, a) => call("agent.act", { block_id: a.block_id, revision: a.revision, send_key: d.send_key, write: d.write, confirm: r.confirm }),
    });
    const seen = new Map();
    return on("terminal.agent_changed", filter, async (c, ev) => {
      if (when.message && !host.re2(when.message, ev.message || "")) return;
      if (cwd && !(ev.cwd || "").startsWith(cwd)) return;
      if (seen.get(ev.block_id) === ev.revision) return;
      seen.set(ev.block_id, ev.revision);
      if (typeof r.filter === "function") {
        const row = (await h.agents.list()).find((a) => a.block_id === ev.block_id);
        if (!row || !(await r.filter(row))) return;
      }
      await call("notify.send", {
        title: r.title || "An agent is waiting",
        body: ev.message || "", target: ev.block_id, level: "warn",
        actions: [{ title: r.button || "Approve", action, args: { block_id: ev.block_id, revision: ev.revision }, step: stepText(d) }],
      });
    }, { self: true });
  }

  const ctx = Object.freeze(JSON.parse(host.ctx));
  const h = {
    ctx,
    args: Object.freeze(JSON.parse(host.args)),
    call,
    log: (level, ...a) => log(level, a),
    after, every, sleep, on, wait, trigger, wait_output,
    once: (event, filter) => wait(event, filter),
    // Keys: bind(key, action, args) or bind(key, fn). Keys as "cmd+shift+t",
    // sequences "ctrl+b>c", in a mode "resize/left".
    bind: (key, target, args) => {
      if (typeof target === "function") {
        const name = (ctx.script + "-key-" + (++keyFns)).replace(/[^a-z0-9_-]/g, "-");
        actions.register({ name, title: "Key " + key, run: (c) => target(c) });
        target = name;
      }
      host.bind(String(key), String(target), args === undefined ? "" : JSON.stringify(args));
    },
    unbind: (key) => host.unbind(String(key)),
    mode: (name, opts) => host.mode(String(name), !!(opts && opts.exclusive)),
    stop: (code) => host.stop(code === undefined ? 0 : code),
    block: (target) => {
      if (target === undefined) {
        if (!ctx.block_id) return Promise.reject(new HiveError({ status: 400, code: "bad_request", message: "no current block: pass a target" }));
        return Promise.resolve(block(ctx.block_id));
      }
      return call("target.resolve", { kind: "block", target: String(target), session_id: ctx.session_id }).then((r) => block(r.id));
    },
    session_of: (target) => {
      if (target === undefined) {
        if (!ctx.session_id) return Promise.reject(new HiveError({ status: 400, code: "bad_request", message: "no current session: pass a target" }));
        return Promise.resolve(session(ctx.session_id));
      }
      return call("target.resolve", { kind: "session", target: String(target) }).then((r) => session(r.id));
    },
    sessions: () => call("session.list", {}).then((r) => r.sessions.map((s) => session(s.session_id))),
    agents: { list: () => call("agent.list", {}).then((r) => r.agents || []), on_state, rule },
    actions,
    // This script's own storage: JSON values by key, kept across reloads
    // and restarts (1 MB).
    store: {
      get: (key) => Promise.resolve().then(() => { const v = host.storeGet(String(key)); return v === "" ? undefined : JSON.parse(v); }),
      set: (key, value) => Promise.resolve().then(() => host.storeSet(String(key), JSON.stringify(value === undefined ? null : value))),
      delete: (key) => Promise.resolve().then(() => host.storeDelete(String(key))),
      keys: (prefix) => Promise.resolve().then(() => JSON.parse(host.storeKeys(prefix === undefined ? "" : String(prefix)))),
    },
  };

  // Namespaces from the server's own method list: h.session.new_split,
  // h.terminal.format (hive.terminal.*), h.whoami.
  for (const name of JSON.parse(host.methods())) {
    const parts = name.startsWith("hive.terminal.") ? ["terminal", name.slice(14)] : name.split(".");
    let o = h;
    for (const p of parts.slice(0, -1)) {
      // A namespace can hang off a handle function: h.block() and h.block.close().
      if ((typeof o[p] !== "object" && typeof o[p] !== "function") || o[p] === null) o[p] = {};
      o = o[p];
    }
    const last = parts[parts.length - 1];
    if (o[last] === undefined) o[last] = (args) => call(name, args);
  }

  const console = {};
  for (const level of ["debug", "info", "warn", "error"]) console[level] = (...a) => log(level, a);
  console.log = (...a) => log("info", a);

  const hive = {
    script: (s) => Object.assign(Object.create(scriptProto), s),
    HiveError,
  };
  const scriptProto = {};

  globalThis.hive = hive;
  globalThis.h = h;
  globalThis.console = console;
  globalThis.HiveError = HiveError;

  // __main runs the bundle: the top level, then a default export that is a
  // function (called with h) or a hive.script (its setup). Resolves to the
  // result, as JSON.
  globalThis.__main = function (bundle, ok, fail) {
    bundle().then((ex) => {
      const d = ex.default;
      if (typeof d === "function") return d(h);
      if (d && Object.getPrototypeOf(d) === scriptProto) {
        if (!host.manifest(JSON.stringify(Object.assign({}, d, { setup: undefined, teardown: undefined })))) return undefined;
        if (typeof d.teardown === "function") host.teardown(() => d.teardown(h));
        if (typeof d.setup === "function") return d.setup(h);
      }
      return undefined;
    }).then(
      (v) => ok(v === undefined ? "" : JSON.stringify(v)),
      (e) => fail(e instanceof Error ? (e.stack || String(e)) : String(e), (e && e.code) || "error"));
  };
})();
