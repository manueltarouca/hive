# Contributing

Thanks for helping. Bug reports, fixes and docs are all welcome.

## Before you start

- For anything bigger than a small fix, open an issue first and say what you
  want to change. We'll agree on the shape before you spend time on it.
- Command names, flags, exit codes, API methods and event names are a
  contract (see [CHANGELOG.md](CHANGELOG.md)). A change that breaks one needs
  a good reason and a changelog entry.

## Build and test

You need Go 1.27 and Zig 0.16 exactly. The Mac app needs Xcode 27; the
Android app needs the Android SDK, NDK 28.2 and JDK 21.

```sh
make vt          # once: Ghostty's terminal library
make build       # ./bin/hive
make test        # the server and the CLI
cd apps/macos && swift test                 # the Mac app's logic
apps/macos/script/bundle.sh                 # apps/macos/build/Hive.app
make vt-android && (cd apps/android && ./gradlew testDebugUnitTest assembleDebug)
```

Tests run against throwaway servers. Never point a test at your own: set
`HIVE_SERVER`, `HIVE_STATE_DIR` and `HIVE_CONFIG` together to a temporary
folder.

A fix comes with a test that fails without it.

## Changes to the apps

Show what changed. For the Mac app, `apps/macos/script/shot.sh` captures the
app on a made-up session, in both themes. For Android,
`apps/android/script/e2e.py` drives the app on an emulator and saves
screenshots. Look at each state: empty, loading, an error, long text, and
both themes.

## Writing

Docs, messages and labels follow the voice in
[.github/BRAND.md](.github/BRAND.md): short sentences, common words, say
what it does. Claim only what exists.

## Using coding agents

You can use them; hive is built for that. Say so in the pull request, and
make sure you understand and have run every change you send.
[AGENTS.md](AGENTS.md) has the notes agents need.

## Pull requests

Keep each one to one change. Fill in the template: what changed, how you
tested it, and the changelog line. Commits use plain English: what changed
and why.
