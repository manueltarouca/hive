# Notes for coding agents

hive is a terminal multiplexer for coding agents: a Go server and CLI
(`cmd/hive`, `internal/`), a Mac app (`apps/macos`, Swift and Metal), an
Android app (`apps/android`, Kotlin and Compose), a site with the docs
(`site/`, `docs/*.md`), and agent skills (`skills/`).

## Build and test

```sh
make vt && make build && make test
cd apps/macos && swift test
cd apps/android && ./gradlew testDebugUnitTest   # after make vt-android
```

## Rules

- Never use the user's own server. A test or an experiment sets
  `HIVE_SERVER`, `HIVE_STATE_DIR` and `HIVE_CONFIG` together, to a temporary
  folder. A server refuses to start on a state folder another server holds.
- Command, flag, method and event names, and exit codes, are a contract. Add
  to them; don't rename or remove them.
- Write a failing test first, then the fix.
- Text the user reads follows [.github/BRAND.md](.github/BRAND.md): short
  sentences, common words, no hype. Never separate things with `·` or ` — `.
- `ARCHITECTURE.md` explains how the parts fit; read it before a change that
  crosses them.
