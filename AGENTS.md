# lazycred contributor guidance

This is a Go credential workbench, not a vault or a cryptographic protocol implementation.
Use the project-local `.agents/skills/go-cli-tui` skill. Use Traditional Chinese
for maintained user guides; commands and compact terminal labels use English.

- Keep CLI/TUI on shared services. Never perform blocking I/O in Update/View.
- Give each async operation a source/selection generation; reject stale successes and errors.
- Use official bw / YubiKey Manager engines. Secrets cross stdin or child-only env,
  never argv, logs, fixtures or configuration. JSON output must remain redacted.
- Never use real vaults or write to attached hardware in development/tests.
- Demo mode must not discover real backends or touch the actual clipboard.
- Run `make check`, `make race`, and `make pty` for relevant changes.
- Build the real main package `./cmd/lazycred`. Do not publish personal records,
  raw transcripts or private repository references. No automatic dependency installs.
- First release is source-only. Update via version-qualified native `go install`;
  an in-process updater is intentionally out of scope.
