# hive: threat model

hive is a terminal multiplexer. A server owns terminals (a PTY each) and runs
programs as the user who started it. Clients (the `hive` CLI, a macOS app, an
Android app) talk to it over HTTP: one JSON call per request, an event
stream, and a WebSocket attach stream. Anyone the server lets in can type
into every terminal and start programs: being let in is the same as being
the user.

## Trust boundaries and entry points

| Entry point | Who can reach it | What hive must ensure |
| --- | --- | --- |
| Unix socket (`$TMPDIR/hive-$UID/server.sock`, `$XDG_RUNTIME_DIR/hive`) | Local processes | Only the owning UID: the socket is 0600 in a 0700 folder, and the peer's credentials are checked (`internal/remote/peer_*.go`) |
| `tailnet://:PORT`, `tailnet-serve:///PATH` | Devices on the user's tailnet | Each request's caller is identified through Tailscale's local API, per request; only the server's owner gets in unless `--tailnet-allow-identified` is given. Identity headers are trusted only on the Serve socket |
| `--tailscale` (tsnet) | The tailnet | Same as above; the node key lives in the state folder (0700) |
| Plain `HOST:PORT` | Anyone on the network | Refused unless `--allow-anonymous`: then everyone is let in, by design (out of scope) |
| Browsers | Any web page the user visits | Requests with `Origin` or `Sec-Fetch-*` are refused; on plain HTTP the `Host` must be the server's own name, against DNS rebinding (`internal/remote/remote.go`) |

## Untrusted input

- **Program output.** Anything a program prints goes through libghostty-vt.
  Escape sequences must not let a program act outside its terminal: the
  clipboard (OSC 52) is written only from the focused pane, and links are
  opened only on the user's click, never executables (apps).
- **Requests.** JSON is decoded strictly (unknown fields are errors); targets
  are resolved without paths; `--cwd` must be absolute or `~`.
- **Saved state.** `state.json`, terminal snapshots, `hosts.json` and the
  settings file are read at start. They are the user's own files, but a
  crash or a partial write must not make the server run something it
  shouldn't: restore runs only a shell or a known agent's resume command,
  with session IDs limited to `[A-Za-z0-9._-]`.
- **Agent hooks.** `hive agent hook` reads JSON from an agent on stdin and
  reports to the server; it runs inside the pane, as the user.
- **Clients.** The apps draw what a server sends. A server is trusted by the
  user who added it, but the apps must not crash or run anything from it.

## Out of scope

- `--allow-anonymous` listeners (they let everyone in, by design).
- A local attacker who is already the same user.
- The programs the user runs in hive.
