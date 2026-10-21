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
      readonly origin: "cli" | "event" | "timer";
      readonly principal: string;
      readonly server: string;
      readonly run_id?: string;
      readonly depth: number;
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
      agents: { list(): Promise<Record<string, any>[]> };

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
