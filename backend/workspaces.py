"""Normalize desktop workspaces for the Workspace picker overview card.

The module intentionally keeps all platform-specific commands here. The browser
only receives a normalized list and sends back the ID of a workspace from that
list; activation commands are rebuilt from the current enumeration instead of
trusting arbitrary command text from the card.
"""
from __future__ import annotations

import json
import os
import re
import shutil
import subprocess
import sys
from pathlib import Path
from typing import Any


class WorkspaceError(RuntimeError):
    """A user-facing workspace adapter error."""


def _run(command: list[str], *, env: dict[str, str] | None = None, timeout: float = 4.0) -> str:
    try:
        result = subprocess.run(
            command,
            capture_output=True,
            text=True,
            timeout=timeout,
            env=env,
            check=False,
        )
    except FileNotFoundError as error:
        raise WorkspaceError(f"{command[0]} is not installed") from error
    except subprocess.TimeoutExpired as error:
        raise WorkspaceError(f"{command[0]} timed out") from error
    except OSError as error:
        raise WorkspaceError(f"Could not run {command[0]}: {error}") from error
    if result.returncode != 0:
        detail = (result.stderr or result.stdout or "").strip()
        raise WorkspaceError(detail[-500:] or f"{command[0]} exited with {result.returncode}")
    return result.stdout


def _json(text: str, label: str) -> Any:
    try:
        return json.loads(text or "null")
    except (TypeError, ValueError) as error:
        raise WorkspaceError(f"Could not parse {label}: {error}") from error


def _safe_text(value: Any, limit: int = 160) -> str:
    return ("" if value is None else str(value)).replace("\x00", "").strip()[:limit]


def _int_or_none(value: Any) -> int | None:
    try:
        return int(value)
    except (TypeError, ValueError):
        return None


def _lua_string(value: Any) -> str:
    text = _safe_text(value)
    escaped = (
        text.replace("\\", "\\\\")
        .replace("\"", "\\\"")
        .replace("\n", "\\n")
        .replace("\r", "\\r")
        .replace("\t", "\\t")
    )
    return f'"{escaped}"'


def _app_name(identifier: Any, title: Any) -> str:
    ident = _safe_text(identifier, 100).split(".desktop", 1)[0]
    title = _safe_text(title)
    if ident:
        low = ident.casefold()
        if low.startswith(("chrome-", "chromium-", "firefox-", "brave-")):
            ident = title.split(" - ", 1)[0] if title else ""
        elif ident.count(".") >= 2 and ident.split(".", 1)[0].casefold() in {
            "app", "com", "dev", "io", "net", "org",
        }:
            ident = ident.rsplit(".", 1)[-1]
        ident = ident.replace("__", " ").strip()
        if ident:
            return ident[:48]
    if title:
        return title.split(" - ", 1)[0][:48]
    return "App"


def _add_app(bucket: dict[str, Any], identifier: Any, title: Any) -> None:
    entry = {
        "name": _app_name(identifier, title),
        "title": _safe_text(title),
    }
    key = entry["name"].casefold()
    if key in bucket["seen"]:
        return
    bucket["seen"].add(key)
    bucket["apps"].append(entry)


def _new_bucket() -> dict[str, Any]:
    return {"windows": 0, "apps": [], "seen": set()}


def _bucket(groups: dict[str, dict[str, Any]], key: str) -> dict[str, Any]:
    if key not in groups:
        groups[key] = _new_bucket()
    return groups[key]


def _workspace(
    workspace_id: Any,
    *,
    name: str = "",
    label: str = "",
    monitor: str = "",
    active: bool = False,
    windows: int = 0,
    apps: list[dict[str, str]] | None = None,
    kind: str = "workspace",
    number: int | None = None,
    preview: str = "",
) -> dict[str, Any]:
    workspace_id = _safe_text(workspace_id, 100)
    name = _safe_text(name or workspace_id, 100)
    return {
        "id": workspace_id,
        "name": name,
        "label": _safe_text(label or name or workspace_id, 100),
        "monitor": _safe_text(monitor, 80),
        "active": bool(active),
        "windows": max(0, int(windows or 0)),
        "apps": (apps or [])[:8],
        "kind": kind,
        "number": number,
        "preview": _safe_text(preview),
    }


def _sort_key(workspace: dict[str, Any]) -> tuple[int, int, str]:
    if workspace.get("kind") == "special":
        return (1, 10**9, workspace.get("label", "").casefold())
    number = workspace.get("number")
    if not isinstance(number, int):
        number = _int_or_none(workspace.get("id"))
    if not isinstance(number, int):
        number = 10**9
    return (0, number, workspace.get("label", "").casefold())


def _payload(
    backend: str,
    desktop: str,
    workspaces: list[dict[str, Any]],
    current: Any = "",
    *,
    platform: str | None = None,
) -> dict[str, Any]:
    return {
        "ok": True,
        "available": True,
        "backend": backend,
        "desktop": desktop,
        "platform": platform or _platform_name(),
        "current": _safe_text(current, 100),
        "workspaces": sorted(workspaces, key=_sort_key),
        "error": "",
        "hint": "",
    }


def _platform_name() -> str:
    if sys.platform == "darwin":
        return "macos"
    if os.name == "nt":
        return "windows"
    if sys.platform.startswith("linux"):
        return "linux"
    return sys.platform


def _desktop_name() -> str:
    value = os.environ.get("XDG_CURRENT_DESKTOP") or os.environ.get("DESKTOP_SESSION") or ""
    return _safe_text(value.split(":", 1)[0], 80) or _platform_name()


def _unsupported(message: str, hint: str = "") -> dict[str, Any]:
    desktop = _desktop_name()
    return {
        "ok": True,
        "available": False,
        "backend": "unsupported",
        "desktop": desktop,
        "platform": _platform_name(),
        "current": "",
        "workspaces": [],
        "error": _safe_text(message, 500),
        "hint": _safe_text(hint, 500),
    }


def _runtime_dir() -> Path:
    return Path(os.environ.get("XDG_RUNTIME_DIR") or f"/run/user/{os.getuid()}")


def _hyprland_envs() -> list[dict[str, str]]:
    base = os.environ.copy()
    base.setdefault("XDG_RUNTIME_DIR", str(_runtime_dir()))
    variants: list[dict[str, str]] = []
    seen: set[str] = set()

    def add(env: dict[str, str]) -> None:
        signature = env.get("HYPRLAND_INSTANCE_SIGNATURE", "")
        if signature in seen:
            return
        seen.add(signature)
        variants.append(env)

    if base.get("HYPRLAND_INSTANCE_SIGNATURE"):
        add(base)
    hypr_dir = _runtime_dir() / "hypr"
    for path in sorted(hypr_dir.glob("*"), key=lambda item: item.stat().st_mtime, reverse=True):
        if not (path / ".socket.sock").exists():
            continue
        env = base.copy()
        env["HYPRLAND_INSTANCE_SIGNATURE"] = path.name
        add(env)
    if not variants:
        variants.append(base)
    return variants


def _socket_env(variable: str, pattern: str) -> dict[str, str]:
    env = os.environ.copy()
    env.setdefault("XDG_RUNTIME_DIR", str(_runtime_dir()))
    if env.get(variable):
        return env
    candidates = sorted(_runtime_dir().glob(pattern), key=lambda item: item.stat().st_mtime, reverse=True)
    if candidates:
        env[variable] = str(candidates[0])
    return env


def build_hyprland_payload(
    workspaces: Any,
    clients: Any,
    active: Any,
    desktop: str = "Hyprland",
) -> dict[str, Any]:
    if not isinstance(workspaces, list):
        raise WorkspaceError("Hyprland did not return a workspace list")
    if not isinstance(clients, list):
        clients = []
    active_id = ""
    if isinstance(active, dict):
        active_id = _safe_text(active.get("id"))
    groups: dict[str, dict[str, Any]] = {}
    for client in clients:
        if not isinstance(client, dict):
            continue
        workspace = client.get("workspace") if isinstance(client.get("workspace"), dict) else {}
        workspace_id = workspace.get("id")
        if workspace_id is None:
            continue
        key = str(workspace_id)
        bucket = _bucket(groups, key)
        bucket["windows"] += 1
        _add_app(
            bucket,
            client.get("class") or client.get("initialClass"),
            client.get("title"),
        )
    normalized: list[dict[str, Any]] = []
    for item in workspaces:
        if not isinstance(item, dict):
            continue
        workspace_id = item.get("id")
        if workspace_id is None:
            continue
        raw_name = _safe_text(item.get("name") or workspace_id, 100)
        special = (isinstance(workspace_id, int) and workspace_id < 0) or raw_name.startswith("special:")
        label = raw_name
        if special:
            label = "Scratchpad" if raw_name == "special:scratchpad" else raw_name.split(":", 1)[-1] or "Special"
        data = groups.get(str(workspace_id), _new_bucket())
        count = item.get("windows")
        if not isinstance(count, int):
            count = data["windows"]
        normalized.append(_workspace(
            workspace_id,
            name=raw_name,
            label=label,
            monitor=item.get("monitor"),
            active=str(workspace_id) == active_id,
            windows=count,
            apps=data["apps"],
            kind="special" if special else "workspace",
            number=workspace_id if isinstance(workspace_id, int) and workspace_id >= 0 else None,
            preview=item.get("lastwindowtitle"),
        ))
    return _payload("hyprland", desktop, normalized, active_id)


def _collect_i3_windows(node: Any, workspace: str = "", groups: dict[str, dict[str, Any]] | None = None) -> dict[str, dict[str, Any]]:
    if groups is None:
        groups = {}
    if not isinstance(node, dict):
        return groups
    if node.get("type") == "workspace":
        workspace = _safe_text(node.get("name") or node.get("num"), 100)
    if workspace and (
        node.get("window") is not None
        or node.get("app_id")
        or node.get("window_properties")
    ):
        properties = node.get("window_properties") if isinstance(node.get("window_properties"), dict) else {}
        bucket = _bucket(groups, workspace)
        bucket["windows"] += 1
        _add_app(
            bucket,
            node.get("app_id") or properties.get("class") or properties.get("instance"),
            node.get("name") or properties.get("title"),
        )
    for child in list(node.get("nodes") or []) + list(node.get("floating_nodes") or []):
        _collect_i3_windows(child, workspace, groups)
    return groups


def build_i3_payload(workspaces: Any, tree: Any, desktop: str) -> dict[str, Any]:
    if not isinstance(workspaces, list):
        raise WorkspaceError(f"{desktop} did not return a workspace list")
    groups = _collect_i3_windows(tree)
    normalized: list[dict[str, Any]] = []
    current = ""
    for item in workspaces:
        if not isinstance(item, dict):
            continue
        name = _safe_text(item.get("name") or item.get("num"), 100)
        number = _int_or_none(item.get("num"))
        data = groups.get(name, _new_bucket())
        active = bool(item.get("focused"))
        if active:
            current = name
        normalized.append(_workspace(
            name or number,
            name=name,
            label=name or (f"Workspace {number}" if number is not None else "Workspace"),
            monitor=item.get("output"),
            active=active,
            windows=data["windows"],
            apps=data["apps"],
            number=number if number and number > 0 else None,
        ))
    return _payload("i3", desktop, normalized, current)


def parse_wmctrl_desktops(text: str, names: list[str] | None = None) -> tuple[list[dict[str, Any]], str]:
    names = names or []
    workspaces: list[dict[str, Any]] = []
    current = ""
    pattern = re.compile(
        r"^\s*(\d+)\s+([*-])\s+DG:\s*(\S+)\s+VP:\s*(\S+)\s+WA:\s*(\S+)\s+(\S+)\s*(.*)$"
    )
    for line in text.splitlines():
        match = pattern.match(line)
        if not match:
            continue
        index = int(match.group(1))
        active = match.group(2) == "*"
        name = _safe_text(match.group(7), 100)
        if not name or name.upper() == "N/A":
            name = names[index] if index < len(names) else f"Desktop {index + 1}"
        if active:
            current = str(index)
        workspaces.append({
            "index": index,
            "name": name,
            "active": active,
            "geometry": match.group(3),
        })
    return workspaces, current


def parse_wmctrl_windows(text: str) -> dict[str, dict[str, Any]]:
    groups: dict[str, dict[str, Any]] = {}
    for line in text.splitlines():
        parts = line.split(None, 4)
        if len(parts) < 5:
            continue
        desktop = _int_or_none(parts[1])
        if desktop is None or desktop < 0:
            continue
        bucket = _bucket(groups, str(desktop))
        bucket["windows"] += 1
        _add_app(bucket, parts[3].rsplit(".", 1)[-1], parts[4])
    return groups


def build_wmctrl_payload(
    desktops_text: str,
    windows_text: str,
    names: list[str] | None = None,
) -> dict[str, Any]:
    desktops, current = parse_wmctrl_desktops(desktops_text, names)
    groups = parse_wmctrl_windows(windows_text)
    normalized: list[dict[str, Any]] = []
    for desktop in desktops:
        key = str(desktop["index"])
        data = groups.get(key, _new_bucket())
        normalized.append(_workspace(
            key,
            name=desktop["name"],
            label=desktop["name"],
            active=desktop["active"],
            windows=data["windows"],
            apps=data["apps"],
            number=desktop["index"],
        ))
    return _payload("wmctrl", "X11 / wmctrl", normalized, current)


def build_kde_payload(count: int, current: int, desktop: str = "KDE Plasma") -> dict[str, Any]:
    if count < 1:
        raise WorkspaceError("KWin reported no virtual desktops")
    one_based = current >= 1
    current_id = str(current if one_based else current)
    workspaces: list[dict[str, Any]] = []
    for raw_index in range(count):
        desktop_number = raw_index + 1 if one_based else raw_index
        raw = raw_index + 1 if one_based else raw_index
        workspaces.append(_workspace(
            raw,
            name=f"Desktop {desktop_number}",
            label=f"Desktop {desktop_number}",
            active=raw == current,
            number=desktop_number,
        ))
    return _payload("kde", desktop, workspaces, current_id)


def build_yabai_payload(spaces: Any, windows: Any, desktop: str = "macOS Spaces") -> dict[str, Any]:
    if not isinstance(spaces, list):
        raise WorkspaceError("yabai did not return a space list")
    if not isinstance(windows, list):
        windows = []
    groups: dict[str, dict[str, Any]] = {}
    for window in windows:
        if not isinstance(window, dict):
            continue
        space = window.get("space")
        if space is None:
            continue
        bucket = _bucket(groups, str(space))
        bucket["windows"] += 1
        _add_app(bucket, window.get("app"), window.get("title"))
    normalized: list[dict[str, Any]] = []
    current = ""
    for space in spaces:
        if not isinstance(space, dict):
            continue
        index = space.get("index")
        if index is None:
            continue
        data = groups.get(str(index), _new_bucket())
        active = bool(space.get("has-focus"))
        if active:
            current = str(index)
        label = _safe_text(space.get("label")) or f"Space {index}"
        display = space.get("display")
        normalized.append(_workspace(
            index,
            name=label,
            label=label,
            monitor=f"Display {display}" if display is not None else "",
            active=active,
            windows=data["windows"],
            apps=data["apps"],
            number=_int_or_none(index),
        ))
    return _payload("yabai", desktop, normalized, current, platform="macos")


def build_windows_payload(items: Any, desktop: str = "Windows") -> dict[str, Any]:
    if isinstance(items, dict):
        items = [items]
    if not isinstance(items, list):
        raise WorkspaceError("VirtualDesktop did not return a desktop list")
    normalized: list[dict[str, Any]] = []
    current = ""
    for item in items:
        if not isinstance(item, dict):
            continue
        index = _int_or_none(item.get("index"))
        if index is None:
            continue
        apps = []
        for app in item.get("apps") or []:
            if isinstance(app, dict):
                apps.append({
                    "name": _safe_text(app.get("name"), 48) or "App",
                    "title": _safe_text(app.get("title")),
                })
            elif app:
                apps.append({"name": _safe_text(app, 48), "title": ""})
        active = bool(item.get("current"))
        if active:
            current = str(index)
        label = _safe_text(item.get("name")) or f"Desktop {index + 1}"
        normalized.append(_workspace(
            index,
            name=label,
            label=label,
            active=active,
            windows=len(apps),
            apps=apps,
            number=index + 1,
        ))
    return _payload("windows", desktop, normalized, current, platform="windows")


class WorkspaceBackend:
    key = "unsupported"
    label = "Unsupported desktop"

    def snapshot(self) -> dict[str, Any]:
        raise NotImplementedError

    def activate(self, workspace: dict[str, Any]) -> None:
        raise NotImplementedError


class HyprlandBackend(WorkspaceBackend):
    key = "hyprland"
    label = "Hyprland"

    def __init__(self, env: dict[str, str]) -> None:
        self.env = env

    @classmethod
    def detect(cls) -> "HyprlandBackend | None":
        if not shutil.which("hyprctl"):
            return None
        for env in _hyprland_envs():
            try:
                _run(["hyprctl", "-j", "activeworkspace"], env=env, timeout=2)
                return cls(env)
            except WorkspaceError:
                continue
        return None

    def snapshot(self) -> dict[str, Any]:
        workspaces = _json(_run(["hyprctl", "-j", "workspaces"], env=self.env), "Hyprland workspaces")
        clients = _json(_run(["hyprctl", "-j", "clients"], env=self.env), "Hyprland clients")
        active = _json(_run(["hyprctl", "-j", "activeworkspace"], env=self.env), "Hyprland active workspace")
        return build_hyprland_payload(workspaces, clients, active)

    def activate(self, workspace: dict[str, Any]) -> None:
        workspace_id = _safe_text(workspace.get("id"))
        name = _safe_text(workspace.get("name") or workspace_id)
        if workspace.get("kind") == "special":
            special = name.split(":", 1)[-1] if ":" in name else name
            modern = f"hl.dsp.workspace.toggle_special({_lua_string(special)})"
            legacy = ["hyprctl", "dispatch", "togglespecialworkspace", special]
        elif workspace_id.lstrip("-").isdigit():
            modern = f"hl.dsp.focus({{ workspace = {workspace_id} }})"
            legacy = ["hyprctl", "dispatch", "workspace", workspace_id]
        else:
            modern = f"hl.dsp.focus({{ workspace = {_lua_string(name)} }})"
            legacy = ["hyprctl", "dispatch", "workspace", f"name:{name}"]
        try:
            _run(["hyprctl", "dispatch", modern], env=self.env)
        except WorkspaceError:
            # Hyprland before the Lua dispatcher migration used the older
            # `dispatch workspace` syntax. Try that only after the modern
            # expression is rejected.
            _run(legacy, env=self.env)


class I3LikeBackend(WorkspaceBackend):
    binary = ""
    raw_output = False

    def __init__(self, env: dict[str, str] | None = None) -> None:
        self.env = env or os.environ.copy()

    def _query(self, kind: str) -> str:
        command = [self.binary]
        if self.raw_output:
            command.append("-r")
        command.extend(["-t", kind])
        return _run(command, env=self.env)

    def snapshot(self) -> dict[str, Any]:
        workspaces = _json(self._query("get_workspaces"), f"{self.label} workspaces")
        tree = _json(self._query("get_tree"), f"{self.label} window tree")
        return build_i3_payload(workspaces, tree, self.label)

    def activate(self, workspace: dict[str, Any]) -> None:
        number = workspace.get("number")
        if isinstance(number, int) and number > 0:
            command = [self.binary, "workspace", "number", str(number)]
        else:
            command = [self.binary, "workspace", _safe_text(workspace.get("name") or workspace.get("id"))]
        _run(command, env=self.env)


class SwayBackend(I3LikeBackend):
    binary = "swaymsg"
    raw_output = True
    key = "sway"
    label = "Sway"

    @classmethod
    def detect(cls) -> "SwayBackend | None":
        if not shutil.which(cls.binary):
            return None
        env = _socket_env("SWAYSOCK", "sway-ipc.*.sock")
        try:
            _run([cls.binary, "-r", "-t", "get_workspaces"], env=env, timeout=2)
            return cls(env)
        except WorkspaceError:
            return None


class I3Backend(I3LikeBackend):
    binary = "i3-msg"
    key = "i3"
    label = "i3"

    @classmethod
    def detect(cls) -> "I3Backend | None":
        if not shutil.which(cls.binary):
            return None
        try:
            _run([cls.binary, "-t", "get_workspaces"], timeout=2)
            return cls()
        except WorkspaceError:
            return None


def _x11_desktop_names() -> list[str]:
    if not shutil.which("xprop"):
        return []
    try:
        output = _run(["xprop", "-root", "_NET_DESKTOP_NAMES"], timeout=2)
    except WorkspaceError:
        return []
    match = re.search(r"=\s*(.*)", output)
    if not match:
        return []
    return [value.replace(r"\"", '"').replace(r"\\", "\\") for value in re.findall(r'"((?:\\.|[^"\\])*)"', match.group(1))]


class WmctrlBackend(WorkspaceBackend):
    key = "wmctrl"
    label = "X11"

    @classmethod
    def detect(cls) -> "WmctrlBackend | None":
        if not shutil.which("wmctrl") or not os.environ.get("DISPLAY"):
            return None
        try:
            _run(["wmctrl", "-d"], timeout=2)
            return cls()
        except WorkspaceError:
            return None

    def snapshot(self) -> dict[str, Any]:
        desktops = _run(["wmctrl", "-d"])
        windows = _run(["wmctrl", "-l", "-x"])
        return build_wmctrl_payload(desktops, windows, _x11_desktop_names())

    def activate(self, workspace: dict[str, Any]) -> None:
        index = workspace.get("number")
        if not isinstance(index, int) or index < 0:
            raise WorkspaceError("This X11 desktop does not have a valid index")
        _run(["wmctrl", "-s", str(index)])


class KdeBackend(WorkspaceBackend):
    key = "kde"
    label = "KDE Plasma"

    def __init__(self, command: str) -> None:
        self.command = command

    @staticmethod
    def _integer(command: str, method: str) -> int:
        output = _run([command, "org.kde.KWin", "/KWin", f"org.kde.KWin.{method}"], timeout=2)
        value = _int_or_none(output.strip())
        if value is None:
            raise WorkspaceError(f"KWin returned an invalid {method} value")
        return value

    @classmethod
    def detect(cls) -> "KdeBackend | None":
        command = shutil.which("qdbus6") or shutil.which("qdbus")
        if not command:
            return None
        try:
            count = cls._integer(command, "numberOfDesktops")
            current = cls._integer(command, "currentDesktop")
            if count > 0 and current >= 0:
                return cls(command)
        except WorkspaceError:
            return None
        return None

    def snapshot(self) -> dict[str, Any]:
        count = self._integer(self.command, "numberOfDesktops")
        current = self._integer(self.command, "currentDesktop")
        return build_kde_payload(count, current)

    def activate(self, workspace: dict[str, Any]) -> None:
        index = workspace.get("id")
        try:
            value = int(index)
        except (TypeError, ValueError) as error:
            raise WorkspaceError("This KDE desktop does not have a valid index") from error
        _run([self.command, "org.kde.KWin", "/KWin", "org.kde.KWin.setCurrentDesktop", str(value)], timeout=2)


class YabaiBackend(WorkspaceBackend):
    key = "yabai"
    label = "macOS Spaces"

    @classmethod
    def detect(cls) -> "YabaiBackend | None":
        if sys.platform != "darwin" or not shutil.which("yabai"):
            return None
        try:
            _run(["yabai", "-m", "query", "--spaces"], timeout=3)
            return cls()
        except WorkspaceError:
            return None

    def snapshot(self) -> dict[str, Any]:
        spaces = _json(_run(["yabai", "-m", "query", "--spaces"], timeout=3), "yabai spaces")
        windows = _json(_run(["yabai", "-m", "query", "--windows"], timeout=3), "yabai windows")
        return build_yabai_payload(spaces, windows)

    def activate(self, workspace: dict[str, Any]) -> None:
        index = _safe_text(workspace.get("id"))
        if not index.lstrip("-").isdigit():
            raise WorkspaceError("This macOS Space does not have a valid index")
        _run(["yabai", "-m", "space", "--focus", index], timeout=3)


POWERSHELL_SNAPSHOT = r"""
$ErrorActionPreference = 'SilentlyContinue'
$count = [int](Get-DesktopCount)
$appsByDesktop = @{}
foreach ($process in @(Get-Process)) {
  $handle = $process.MainWindowHandle
  if (-not $handle -or $handle -eq 0) { continue }
  try {
    $desktop = Get-DesktopFromWindow -Hwnd $handle
    $index = [int](Get-DesktopIndex -Desktop $desktop)
    if ($index -ge 0) {
      if (-not $appsByDesktop.ContainsKey($index)) { $appsByDesktop[$index] = @() }
      $appsByDesktop[$index] += [pscustomobject]@{
        name = [string]$process.ProcessName
        title = [string]$process.MainWindowTitle
      }
    }
  } catch {}
}
$list = @()
for ($i = 0; $i -lt $count; $i++) {
  $desktop = Get-Desktop $i
  $name = ''
  try { $name = [string](Get-DesktopName -Desktop $desktop) } catch {}
  $current = $false
  try { $current = [bool](Test-CurrentDesktop -Desktop $desktop) } catch {}
  $apps = @()
  if ($appsByDesktop.ContainsKey($i)) { $apps = @($appsByDesktop[$i]) }
  $list += [pscustomobject]@{
    index = $i
    name = $name
    current = $current
    apps = $apps
  }
}
ConvertTo-Json -InputObject @($list) -Compress -Depth 5
"""


class WindowsBackend(WorkspaceBackend):
    key = "windows"
    label = "Windows"

    def __init__(self, powershell: str) -> None:
        self.powershell = powershell

    @classmethod
    def detect(cls) -> "WindowsBackend | None":
        if os.name != "nt":
            return None
        command = shutil.which("powershell") or shutil.which("pwsh")
        if not command:
            return None
        try:
            output = _run([
                command,
                "-NoProfile",
                "-NonInteractive",
                "-Command",
                "if (Get-Command Get-DesktopCount -ErrorAction SilentlyContinue) { 'yes' }",
            ], timeout=5)
            if "yes" not in output:
                return None
            return cls(command)
        except WorkspaceError:
            return None

    def snapshot(self) -> dict[str, Any]:
        output = _run([
            self.powershell,
            "-NoProfile",
            "-NonInteractive",
            "-Command",
            POWERSHELL_SNAPSHOT,
        ], timeout=10)
        return build_windows_payload(_json(output, "VirtualDesktop desktops"))

    def activate(self, workspace: dict[str, Any]) -> None:
        index = workspace.get("id")
        try:
            value = int(index)
        except (TypeError, ValueError) as error:
            raise WorkspaceError("This Windows desktop does not have a valid index") from error
        _run([
            self.powershell,
            "-NoProfile",
            "-NonInteractive",
            "-Command",
            f"Switch-Desktop -Desktop {value}",
        ], timeout=10)


def detect_backend() -> WorkspaceBackend | None:
    if sys.platform.startswith("linux"):
        for backend in (HyprlandBackend.detect(), SwayBackend.detect(), I3Backend.detect(), WmctrlBackend.detect(), KdeBackend.detect()):
            if backend is not None:
                return backend
        return None
    if sys.platform == "darwin":
        return YabaiBackend.detect()
    if os.name == "nt":
        return WindowsBackend.detect()
    return None


def _unsupported_hint() -> str:
    if sys.platform == "darwin":
        return "Install yabai and enable its scripting addition to expose macOS Spaces."
    if os.name == "nt":
        return "Install the VirtualDesktop PowerShell module (PSVirtualDesktop) to expose Windows virtual desktops."
    if sys.platform.startswith("linux"):
        return (
            "Supported Linux controllers: Hyprland, Sway, i3, KDE Plasma, "
            "or any EWMH X11 window manager with wmctrl."
        )
    return "This platform does not expose a supported workspace controller."


class WorkspaceManager:
    def snapshot(self) -> dict[str, Any]:
        backend = detect_backend()
        if backend is None:
            return _unsupported("No supported workspace controller was found.", _unsupported_hint())
        try:
            return backend.snapshot()
        except WorkspaceError as error:
            return _unsupported(str(error), _unsupported_hint())
        except (OSError, ValueError) as error:
            return _unsupported(f"Workspace backend failed: {error}", _unsupported_hint())

    def activate(self, workspace_id: str) -> dict[str, Any]:
        backend = detect_backend()
        if backend is None:
            return {
                "ok": False,
                "available": False,
                "error": "No supported workspace controller was found.",
                "hint": _unsupported_hint(),
            }
        try:
            snapshot = backend.snapshot()
            target = next(
                (workspace for workspace in snapshot.get("workspaces", [])
                 if _safe_text(workspace.get("id")) == _safe_text(workspace_id)),
                None,
            )
            if target is None:
                return {
                    "ok": False,
                    "available": True,
                    "error": "That workspace is no longer open.",
                }
            backend.activate(target)
            return {"ok": True, "available": True, "workspace": target}
        except WorkspaceError as error:
            return {"ok": False, "available": True, "error": str(error)}
        except (OSError, ValueError) as error:
            return {"ok": False, "available": True, "error": f"Could not switch workspace: {error}"}
