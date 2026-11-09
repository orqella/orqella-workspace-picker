# Workspace picker module

Dashboard-only Orqella module. It has no page entry; the overview card is the
whole user interface.

- `backend/workspaces.go` owns desktop detection, normalization, and switching.
- `backend/main.go` is the authenticated native sidecar API.
- `ui/overview.js` renders the card and only sends workspace IDs back.
- Keep platform command construction in the backend. Never let the browser send
  command text, flags, or arguments.
- macOS tries AeroSpace first, then yabai. Keep both adapters independent and
  degrade gracefully when neither is running.
- Match an activation request against the current workspace snapshot before
  running a command; reject unknown or stale IDs.
- The sidecar runs unsandboxed because it needs the graphical session. Keep the
  command set limited to the supported workspace controllers and do not add
  arbitrary shell execution.
- Do not make the module write files or collect telemetry. There is no network
  access requirement.
- Prefer useful workspace metadata where the desktop exposes it. Do not invent
  app names or monitor labels when a backend cannot provide them.
- `backend/vendor/psvirtualdesktop/` is an unmodified MIT-licensed vendored
  dependency. Keep its license and files together; do not edit the vendored
  script.
- Release binaries are published to the public
  `orqella/workspace-picker-artifacts` repository. The module source stays
  private; keep the artifact checksums in `module.toml` in sync with the
  published binaries.
- Add parser tests for every new backend under `backend/workspaces_test.go`.
- Load `/assets/theme.css` and `/assets/module.css` before `overview.css`; use
  core primitives and theme tokens.
- Keep the card usable on narrow phones and for long workspace names.
- Run `go test ./...`, build the release artifacts, and run
  `orqella module check .` before publishing.
