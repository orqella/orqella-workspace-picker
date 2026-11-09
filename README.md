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

## How it works

The module has a small Python sidecar. It detects the running desktop and uses
its native workspace controller:

| Environment | Workspace source | Switching | App names |
| --- | --- | --- | --- |
| Hyprland | `hyprctl workspaces` / `clients` | `hyprctl dispatch workspace` | Yes |
| Sway | `swaymsg get_workspaces` / `get_tree` | `swaymsg workspace` | Yes |
| i3 | `i3-msg get_workspaces` / `get_tree` | `i3-msg workspace` | Yes |
| KDE Plasma | KWin D-Bus desktop count/current desktop | `setCurrentDesktop` | No |
| Other EWMH X11 WMs | `wmctrl -d` / `wmctrl -l -x` | `wmctrl -s` | Yes |
| macOS + yabai | `yabai query --spaces` / `--windows` | `yabai space --focus` | Yes |
| Windows + PSVirtualDesktop | `Get-Desktop*` PowerShell module | `Switch-Desktop` | Yes |

Where a desktop only exposes workspace numbers, the card shows the number and
says what it can. Unsupported environments return a clear message instead of a
broken button. GNOME Wayland and a bare Windows install do not currently expose
a stable workspace CLI; Windows support requires the third-party
[PSVirtualDesktop](https://github.com/MScholtes/PSVirtualDesktop) PowerShell
module.

The sidecar is intentionally unsandboxed (`sandbox = false`) because it must
talk to the logged-in graphical session. It only runs `hyprctl`, `swaymsg`,
`i3-msg`, `wmctrl`, `qdbus`, `yabai`, or PowerShell commands, and it never
accepts command text from the browser. The browser chooses a workspace ID from
the latest snapshot; the sidecar validates that ID before switching.

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
orqella module check .
python3 -m unittest discover -s tests -v
orqella module dev .
```

The sidecar uses only the Python standard library.
