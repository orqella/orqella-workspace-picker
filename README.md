# Workspace picker

A dashboard-only Orqella module for seeing and switching the open workspaces on
the selected machine. It has no page tab; it appears as an overview card.

The card shows each workspace's number or name, monitor, active state, window
count, and the applications visible on it when the desktop exposes that
information. Click or tap a workspace to switch to it.

## Install

```bash
orqella module install https://github.com/orqella/orqella-workspace-picker
```

Review and grant the module's **backend** permission in **Settings → Modules**
on first install, then enable the card from **Edit** on a dashboard if it is not
already visible. The module is installed with `overview_default = true`.

Prebuilt sidecars are downloaded from the public
[`orqella/workspace-picker-artifacts`](https://github.com/orqella/workspace-picker-artifacts)
repository. The module source remains private; the artifact repository contains
only checksummed binaries and their checksums.

## Requirements

- No Python or other language runtime is required. The module downloads a
  prebuilt native sidecar for the host platform.
- Linux: one of Hyprland, Sway, i3, KDE Plasma, or an EWMH X11 window manager
  with `wmctrl`.
- macOS: [AeroSpace](https://nikitabobko.github.io/AeroSpace/) (recommended) or
  `yabai` for Spaces.
- Windows: Orqella started in the logged-in desktop session. The bundled
  VirtualDesktop helper uses the built-in Windows PowerShell 5.1; no separate
  PowerShell Gallery installation is needed.

## How it works

The module has a small native Go sidecar. It detects the running desktop and
uses its native workspace controller:

| Environment | Workspace source | Switching | App names |
| --- | --- | --- | --- |
| Hyprland | `hyprctl workspaces` / `clients` | `hyprctl dispatch` | Yes |
| Sway | `swaymsg get_workspaces` / `get_tree` | `swaymsg workspace` | Yes |
| i3 | `i3-msg get_workspaces` / `get_tree` | `i3-msg workspace` | Yes |
| KDE Plasma | KWin D-Bus desktop count/current desktop | `setCurrentDesktop` | No |
| Other EWMH X11 WMs | `wmctrl -d` / `wmctrl -l -x` | `wmctrl -s` | Yes |
| macOS + AeroSpace | `aerospace list-workspaces` / `list-windows` | `aerospace workspace` | Yes |
| macOS + yabai | `yabai query --spaces` / `--windows` | `yabai space --focus` | Yes |
| Windows + bundled VirtualDesktop helper | `Get-Desktop*` PowerShell helper | `Switch-Desktop` | Yes |

Where a desktop only exposes workspace numbers, the card shows the number and
says what it can. Unsupported environments return a clear message instead of a
broken button. GNOME Wayland currently has no stable workspace CLI.

AeroSpace is preferred when both macOS backends are available. It avoids
private-API and SIP requirements and gives app names from its own workspace
model. `yabai` remains available as a fallback.

Windows uses a bundled, MIT-licensed copy of
[PSVirtualDesktop](https://github.com/MScholtes/PSVirtualDesktop), so there is no
separate PowerShell Gallery installation step. Orqella must be running in the
logged-in desktop session: a session-0 Windows service or SSH-launched daemon
cannot access virtual desktops. Install/run Orqella with the normal per-user
Windows startup path (`orqella service install`) on real desktops.

The sidecar is intentionally unsandboxed (`sandbox = false`) because it must
talk to the logged-in graphical session. It only runs `hyprctl`, `swaymsg`,
`i3-msg`, `wmctrl`, `qdbus`, `aerospace`, `yabai`, or PowerShell commands, and
it never accepts command text from the browser. The browser chooses a workspace
ID from the latest snapshot; the sidecar validates that ID before switching.

## API

The sidecar is reached through the Orqella `backend.request` bridge:

- `GET /api/workspaces`
- `POST /api/workspaces/activate` with `{ "id": "<workspace id>" }`

The response is normalized:

```json
{
  "available": true,
  "backend": "hyprland",
  "desktop": "Hyprland",
  "current": "1",
  "refresh_ms": 8000,
  "workspaces": [
    {
      "id": "1",
      "name": "1",
      "label": "1",
      "monitor": "DP-1",
      "active": true,
      "windows": 3,
      "apps": [
        { "name": "VSCode", "title": "project - Visual Studio Code" }
      ],
      "kind": "workspace",
      "preview": ""
    }
  ]
}
```

## Development

```bash
cd backend
go test ./...
go build -trimpath -ldflags="-s -w" -o /tmp/workspace-picker .
```

The sidecar is a single static Go binary. `orqella module check .` validates the
manifest and artifact entries. Release artifacts are built for Linux, macOS,
and Windows on x86_64 and aarch64.
