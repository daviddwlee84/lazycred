# Security and data handling

Do not report vulnerabilities with real PINs, seeds, vault exports or session keys.
Use fictional reproductions. Demo mode performs no real backend or clipboard I/O.

- This program delegates cryptography and vault access to official backends.
- Hardware secrets and Bitwarden sessions stay in process memory; they are never
  written to configuration, logs or argv. Child-only environment is used for
  native bw unlock; the YubiKey bridge uses stdin.
- Metadata JSON always redacts fields marked secret. A whole native bw JSON
  response can contain secrets before it is projected into metadata.
- Lock and exit release references; Go/Python garbage collection does not offer
  a guarantee of secure memory erasure. Host compromise remains outside scope.
- Clipboard values are sent through stdin to a bounded cleanup child. Only a
  non-secret owner token is persisted. Cleanup compares owner and current value;
  it cannot erase clipboard-manager history or guarantee cleanup after forced
  termination or operating-system failure.
- Mutations identify a serial and exact item ID, confirm the target and do not
  retry uncertain writes or claim rollback. Keep independent recovery methods.
- Tests must never write to attached hardware or use real vaults. Runtime state,
  screenshots, bug reports and fixtures must not contain personal credentials.

v0.1.0 is an initial integration tool, not an audited password-manager replacement.
