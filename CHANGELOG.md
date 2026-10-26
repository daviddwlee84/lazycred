# Changelog

## Unreleased

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
