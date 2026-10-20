# Security

hive runs programs as you. Anyone a hive server lets in can type into every
terminal and read every screen, so we take reports seriously.

## Reporting

Report a problem privately through
[GitHub's private vulnerability reporting](https://github.com/manueltarouca/hive/security/advisories/new),
or by email to [security@matm.xyz](mailto:security@matm.xyz). Please don't
open a public issue. Say which version (`hive version`), what
an attacker needs (a tailnet login, a local account, a network position),
and what they get.

We answer within a week, and say what we'll do and when.

## In scope

- The API and the attach stream: on the Unix socket, on the tailnet
  listeners, and on built-in Tailscale.
- Who a server lets in: Tailscale identities, owners, tagged devices, and the
  `--tailnet-allow-identified` and `--tailscale-allow-tailnet-members`
  options.
- Requests from browsers: hive refuses them (Origin, content type, `Host`).
- Hive.app and hive for Android: what they do with the text a server sends,
  links, the clipboard, notifications, and the Share Hosts code.
- The release files and their checksums and provenance.

## Not in scope

- `--allow-anonymous`: a plain listener lets in anyone who reaches it, by
  design. It is for tests on one machine.
- Someone who is already you on the machine: your own account can use your
  own server's socket.
- The programs you run in hive, and what they send to the terminal, except
  where hive itself does something unsafe with it.
