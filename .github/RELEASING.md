# Releasing

Releases follow [semver](https://semver.org). The git tag is the version:
`make version` and the app's Info.plist both read it.

## Cut a release

Run these from a hive session.

1. In `CHANGELOG.md`, move the entries under `[Unreleased]` into a new
   section, `## [X.Y.Z] - YYYY-MM-DD`, and update the links at the bottom.
2. Check that a release builds here: `VERSION=X.Y.Z script/release.sh`.
   Then unpack `dist/` and check that `hive version` prints `hive X.Y.Z`.
3. Commit, then tag and push:
   ```sh
   git tag vX.Y.Z
   git push origin main vX.Y.Z
   ```
4. Pushing the tag starts `.github/workflows/release.yml`:
   - It checks that the tag is semver and has a changelog section.
   - It tests and builds on macOS (Xcode 27.1) and Linux (glibc 2.28).
   - It builds and signs the Android app, when the repository has its
     secrets: `ANDROID_KEYSTORE_BASE64` (the release keystore, base64) and
     `HIVE_RELEASE_PW`. The APK takes the release's version, and its version
     code comes from it (0.1.0 is 10099; 0.1.0-rc.1 is 10001). Without the
     secrets, the release has no APK.
   - It creates a draft release, uploads the files, `SHA256SUMS` and the
     provenance attestations, then publishes.

   A tag such as `v0.2.0-rc.1` becomes a pre-release.

## When something fails

- **Before the release job:** nothing is public. Fix the problem, then move
  the tag to the fixed commit:
  ```sh
  git tag -f vX.Y.Z
  git push -f origin vX.Y.Z
  ```
- **During the release job:** the release stays a draft. Re-run the workflow:
  it reuses the draft, replaces the uploads, and publishes.
- **After publishing:** don't move the tag. Release `X.Y.(Z+1)` instead.
  Scripts and checksums may already point at the old files.
- **The `xcode-27` runner image is a preview:** if it fails for reasons that
  aren't ours, switch the macOS entry in the workflow to `macos-26` with
  `/Applications/Xcode_26.6.app`.

## Before notarization

When there is an Apple Developer ID, two things must change:
- Move the bundled `hive` from `Contents/Resources` to `Contents/Helpers`,
  and sign it with its own identifier. Notarization rejects an executable in
  `Resources`.
- Add signing, notarization and stapling to the macOS job.
