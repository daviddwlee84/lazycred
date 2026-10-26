# Changelog

## Unreleased

## v0.1.1 — 2026-10-27

- Recover from CCID touch-eject with `r`: match the exact reader to its readable
  key, then load that source. Refresh no longer races reads of the old source.
- Preserve category/filter/selection for an unchanged key; never guess another
  device when a reader cannot be associated safely.
- Fix pane sizing that wrapped source names into extra rows. Show applicable
  error actions and an explicit Add TOTP hint for an empty OATH category.
- Add mocked discovery/recovery regressions and a real PTY reader-recovery flow.

## v0.1.0 — 2026-10-26

- Add a Go CLI/TUI with source, item and detail panes, metadata search, masked
  fields, explicit copy/reveal, idle lock and a no-I/O demo mode.
- Integrate official YubiKey Manager APIs through a pinned, explicitly prepared
  helper, and the native Bitwarden CLI through JSON and child-only credentials.
- Add confirmed common OATH/FIDO/PIN management and personal TOTP copying with
  destination checks and source retention. Public keys/certificates can be exported.
- Support macOS/Linux clipboard backends with bounded owner-aware cleanup.
- Publish source installation through the actual cmd/lazycred main package.
  Update with native go install; no custom updater or binary-package pipeline.
