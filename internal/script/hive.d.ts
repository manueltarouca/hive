export {};

declare global {
  /** The API: every method by name, handles, events, timers and logs. */
  const h: hive.Hive;
  /** hive.script({...}) declares a script's name and permissions. */
  const hive: { script(s: hive.Script): hive.Script; HiveError: typeof HiveError };
  /** Lines for the script's log: hive script run prints them on stderr. */
  const console: {
    log(...msg: unknown[]): void;
    info(...msg: unknown[]): void;
    warn(...msg: unknown[]): void;
    error(...msg: unknown[]): void;
    debug(...msg: unknown[]): void;
  };
  /** A failed call: the API's error code, HTTP status and message. */
  class HiveError extends Error {
    status: number;
    code: "bad_request" | "not_found" | "method_not_found" | "ambiguous" | "conflict" | "internal"
      | "forbidden" | "permission_denied" | "timeout" | "script_unloaded" | (string & {});
    details?: unknown;
  }

  namespace hive {
    type SessionId = string;
    type BlockId = string;
    /** An ID, a label, a position from 1, or the end of an ID. */
    type Target = string;
    /** Milliseconds, or "250ms", "5s", "10m", "1h". */
    type Duration = number | `${number}${"ms" | "s" | "m" | "h"}`;
    type Permission = "read" | "screen" | "control" | "spawn" | "input" | "ui" | "config" | "server";

    /** Where the script runs, and for a handler, what woke it. */
    interface Context {
      readonly session_id?: SessionId;
      readonly block_id?: BlockId;
      readonly client_id?: string;
      readonly origin: "cli" | "event" | "timer" | "trigger" | "api" | "notification" | "palette" | "key" | "script";
      readonly principal: string;
      readonly server: string;
      readonly run_id?: string;
      readonly depth: number;
      /** This script's name. */
      readonly script: string;
      /** For an event handler: the event's name and number. */
      readonly event?: string;
      readonly seq?: number;
    }

    interface Script {
      /** Letters, digits, - and _. */
      name: string;
      description?: string;
      /** What the script may do. Every script may read. */
      permissions: Permission[];
      setup?(h: Hive): unknown | Promise<unknown>;
      teardown?(h: Hive): unknown | Promise<unknown>;
    }

    interface Subscription { cancel(): void }

    type AgentState = "idle" | "working" | "blocked" | "done" | "ended";
    interface AgentRow {
      block_id: BlockId; session_id: SessionId; session_label?: string; window_id?: string; label?: string; title?: string;
      agent: { harness: string; session_id?: string; cwd?: string; state: AgentState; message?: string;
        kind?: string; revision: number; updated_at: string };
      foreground?: { pid: number; name: string; cwd?: string; program?: string };
    }
    interface Rule {
      /** Lowercase letters, digits, - and _. */
      name: string;
      when: {
        state?: AgentState;
        /** For blocked agents: permission_prompt unless you name another kind. */
        kind?: string;
        harness?: string;
        /** A pattern (RE2) the agent's message must match. */
        message?: string;
        /** The agent's folder starts with this (~ is your home). */
        cwd?: string;
      };
      /** What the button does: press keys, or type text. */
      do: { send_key: string[] } | { write: string };
      /** A pattern (RE2) the pane's last 10 lines must match when the button is pressed. */
      confirm?: string;
      filter?(row: AgentRow): boolean | Promise<boolean>;
      /** Only suggest for now: a notification with a button. */
      mode?: "suggest";
      title?: string;
      button?: string;
    }
    interface ActionSpec<A> {
      /** Lowercase letters, digits, - and _. */
      name: string;
      title?: string;
      description?: string;
      category?: string;
      keywords?: string[];
      /** Other scripts may run it. */
      public?: boolean;
      run(ctx: Context, args: A): unknown | Promise<unknown>;
    }
    interface ActionInfo { name: string; title?: string; description?: string; script: string; public?: boolean }
    interface TriggerSpec {
      /** RE2: no look-behind or back-references. */
      pattern: string;
      flags?: "i" | "m" | "s" | "im" | "is" | "ms" | "ims";
      name?: string;
      /** One block's or one session's output (IDs). Default: every terminal the script hears. */
      block?: BlockId;
      session?: SessionId;
      /** main (default), alt (full-screen programs) or any. */
      screen?: "main" | "alt" | "any";
      /** lines (default), or snapshot: the visible screen, for full-screen programs. */
      mode?: "lines" | "snapshot";
      once?: boolean;
      min_interval?: Duration;
    }
    interface OutputMatch {
      block_id: BlockId; session_id: SessionId; trigger: string;
      match: string; groups: string[]; line: string; screen: "main" | "alt";
    }
    interface Timer { cancel(): void; active(): boolean }

    /** Event fields to match: a value, or a list of values (any of them). */
    type Filter = Record<string, unknown>;
    type Event = Record<string, any> & { block_id?: BlockId; session_id?: string };

    interface Block {
      readonly id: BlockId;
      new_split(o?: { direction?: "horizontal" | "vertical"; side?: "right" | "left" | "below" | "above";
        ratio?: number; layout?: Methods["session.new_split"]["args"]["layout"]; focus?: boolean }): Promise<Block[]>;
      /** The screen and scrollback as text. */
      format(o?: { format?: "text" | "vt"; trim?: boolean; unwrap?: boolean; tail?: number }): Promise<string>;
      /** Type text into the terminal. */
      write(text: string): Promise<void>;
      /** Press keys: "enter", "ctrl+c", "up"... */
      send_key(...keys: string[]): Promise<void>;
      title(): Promise<string>;
      /** The folder the program reported, else its working directory. */
      pwd(): Promise<string>;
      process(): Promise<Methods["hive.terminal.process"]["result"]>;
      /** Wait for the program to exit. No 60 second cap. */
      wait(o?: { timeout?: Duration }): Promise<{ exit_code: number }>;
      /** Read the label, or set it. */
      label(l?: string): Promise<string>;
      zoom(on?: boolean): Promise<void>;
      focus(): Promise<void>;
      close(): Promise<void>;
      /** The agent in this block, if one runs there. */
      agent(): Promise<Record<string, any> | undefined>;
    }

    interface Session {
      readonly id: SessionId;
      label(): Promise<string>;
      view(): Promise<Methods["session.view"]["result"]>;
      new_window(o?: Omit<Methods["session.new_window"]["args"], "session_id">): Promise<Block[]>;
      close(): Promise<void>;
    }

    interface Hive extends Omit<Generated, "block"> {
      readonly ctx: Context;
      /** KEY=VALUE pairs and --args. */
      readonly args: Record<string, unknown>;
      call<M extends keyof Methods>(method: M, args?: Methods[M]["args"]): Promise<Methods[M]["result"]>;
      call(method: string, args?: Record<string, unknown>): Promise<any>;

      /** The current block, or one by target. Also block.close and block.method. */
      block: ((target?: Target) => Promise<Block>) & Generated["block"];
      session_of(target?: Target): Promise<Session>;
      sessions(): Promise<Session[]>;
      agents: {
        list(): Promise<AgentRow[]>;
        /** fn(ctx, row) whenever an agent reaches one of these states. */
        on_state(state: AgentState | AgentState[], fn: (ctx: Context, row: AgentRow) => unknown): Subscription;
        /** Watch for agents blocked on something you'd answer the same way every time, and offer a one-tap answer. */
        rule(r: Rule): Subscription;
      };
      actions: {
        register<A = Record<string, any>>(a: ActionSpec<A>): void;
        list(): Promise<ActionInfo[]>;
        /** Run an action: this script's, or another script's public one. */
        run(name: string, args?: Record<string, unknown>): Promise<any>;
      };
      /** fn(ctx, match) for each line a terminal prints that matches. Needs screen. */
      trigger(t: TriggerSpec, fn: (ctx: Context, m: OutputMatch) => unknown): Subscription;
      /** The next line a terminal prints that matches. Needs screen. */
      wait_output(t: TriggerSpec & { timeout?: Duration }): Promise<OutputMatch>;

      /** Run fn for every event of this name ("*": all). */
      on(event: string, fn: (ctx: Context, ev: Event) => unknown): Subscription;
      /** opts.self: also hear events this script's own calls made. */
      on(event: string, filter: Filter | ((ev: Event, ctx: Context) => boolean), fn: (ctx: Context, ev: Event) => unknown, opts?: { self?: boolean }): Subscription;
      once(event: string, filter?: Filter | ((ev: Event, ctx: Context) => boolean)): Promise<Event>;
      wait(event: string, filter?: Filter | ((ev: Event, ctx: Context) => boolean), opts?: { timeout?: Duration }): Promise<Event>;

      after(d: Duration, fn: (ctx: Context) => unknown): Timer;
      every(d: Duration, fn: (ctx: Context) => unknown): Timer;
      sleep(d: Duration): Promise<void>;
      /** End the script now; a code other than 0 makes it fail. */
      stop(code?: number): never;

      log(level: "debug" | "info" | "warn" | "error", ...msg: unknown[]): void;

      /** Give a key to an action (this script's, or an app's own such as client.new_tab), or to a function. Scripts in the folder only; needs ui. */
      bind(key: string, action: string, args?: Record<string, unknown>): void;
      bind(key: string, fn: (ctx: Context) => unknown): void;
      /** Remove this script's binding of a key. */
      unbind(key: string): number;
      /** Declare a mode: its keys (MODE/KEY) apply once client.mode enters it, until Escape. */
      mode(name: string, opts?: { exclusive?: boolean }): void;

      /** This script's own values, kept across reloads and restarts (1 MB). */
      store: {
        get<T = any>(key: string): Promise<T | undefined>;
        set(key: string, value: unknown): Promise<void>;
        delete(key: string): Promise<void>;
        keys(prefix?: string): Promise<string[]>;
      };
    }
  }
}
