import sys
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT / "backend"))

import workspaces  # noqa: E402


class HyprlandTests(unittest.TestCase):
    def test_normalizes_workspaces_and_apps(self):
        payload = workspaces.build_hyprland_payload(
            [
                {
                    "id": 1,
                    "name": "1",
                    "monitor": "DP-1",
                    "windows": 2,
                    "lastwindowtitle": "orqella - Visual Studio Code",
                },
                {
                    "id": -98,
                    "name": "special:scratchpad",
                    "monitor": "DP-1",
                    "windows": 1,
                    "lastwindowtitle": "Orqella",
                },
            ],
            [
                {"workspace": {"id": 1}, "class": "com.microsoft.VSCode", "title": "orqella - Visual Studio Code"},
                {"workspace": {"id": 1}, "class": "foot", "title": "eolu@host:~/Projects"},
                {"workspace": {"id": -98}, "class": "chrome-orqella.localhost__-Default", "title": "Orqella"},
            ],
            {"id": 1, "name": "1"},
        )

        self.assertTrue(payload["available"])
        self.assertEqual(payload["backend"], "hyprland")
        self.assertEqual(payload["current"], "1")
        self.assertTrue(payload["workspaces"][0]["active"])
        self.assertEqual(payload["workspaces"][0]["apps"][0]["name"], "VSCode")
        self.assertEqual(payload["workspaces"][1]["kind"], "special")
        self.assertEqual(payload["workspaces"][1]["label"], "Scratchpad")


class I3Tests(unittest.TestCase):
    def test_uses_tree_windows_and_focused_workspace(self):
        payload = workspaces.build_i3_payload(
            [
                {"name": "1: web", "num": 1, "output": "eDP-1", "focused": True},
                {"name": "2", "num": 2, "output": "eDP-1", "focused": False},
            ],
            {
                "type": "root",
                "nodes": [{
                    "type": "workspace",
                    "name": "1: web",
                    "nodes": [{
                        "type": "con",
                        "name": "Docs - Chromium",
                        "window": 123,
                        "window_properties": {"class": "chromium"},
                    }],
                }],
            },
            "Sway",
        )

        self.assertEqual(payload["backend"], "i3")
        self.assertEqual(payload["current"], "1: web")
        self.assertEqual(payload["workspaces"][0]["windows"], 1)
        self.assertEqual(payload["workspaces"][0]["apps"][0]["name"], "chromium")
        self.assertTrue(payload["workspaces"][0]["active"])


class WmctrlTests(unittest.TestCase):
    def test_parses_desktops_and_window_groups(self):
        payload = workspaces.build_wmctrl_payload(
            "0  * DG: 1920x1080  VP: 0,0  WA: 0,0 1920,1080  Work\n"
            "1  - DG: 1920x1080  VP: 0,0  WA: 0,0 1920,1080  N/A\n",
            "0x04600007 0 host code.Code Project - Visual Studio Code\n"
            "0x04600008 -1 host sticky.Sticky Sticky window\n",
            ["Work", "Play"],
        )

        self.assertEqual(payload["backend"], "wmctrl")
        self.assertEqual(payload["current"], "0")
        self.assertEqual(payload["workspaces"][0]["label"], "Work")
        self.assertEqual(payload["workspaces"][1]["label"], "Play")
        self.assertEqual(payload["workspaces"][0]["apps"][0]["name"], "Code")
        self.assertEqual(payload["workspaces"][1]["apps"], [])


class OtherBackendTests(unittest.TestCase):
    def test_kde_workspaces_are_numbered(self):
        payload = workspaces.build_kde_payload(4, 2)
        self.assertEqual([item["label"] for item in payload["workspaces"]], [
            "Desktop 1", "Desktop 2", "Desktop 3", "Desktop 4",
        ])
        self.assertEqual(payload["current"], "2")
        self.assertTrue(payload["workspaces"][1]["active"])

    def test_yabai_spaces_include_apps(self):
        payload = workspaces.build_yabai_payload(
            [
                {"index": 1, "label": "Term", "display": 1, "has-focus": True},
                {"index": 2, "label": "", "display": 1, "has-focus": False},
            ],
            [
                {"space": 1, "app": "WezTerm", "title": "shell"},
                {"space": 2, "app": "Safari", "title": "Docs"},
            ],
        )
        self.assertEqual(payload["workspaces"][0]["label"], "Term")
        self.assertEqual(payload["workspaces"][1]["label"], "Space 2")
        self.assertEqual(payload["workspaces"][0]["apps"][0]["name"], "WezTerm")

    def test_windows_desktops_include_names_and_apps(self):
        payload = workspaces.build_windows_payload([
            {"index": 0, "name": "Main", "current": True, "apps": [{"name": "Code", "title": "Project"}]},
            {"index": 1, "name": "", "current": False, "apps": []},
        ])
        self.assertEqual(payload["workspaces"][0]["label"], "Main")
        self.assertEqual(payload["workspaces"][1]["label"], "Desktop 2")
        self.assertTrue(payload["workspaces"][0]["active"])
        self.assertEqual(payload["workspaces"][0]["windows"], 1)

    def test_zero_identifiers_are_preserved(self):
        self.assertEqual(workspaces._safe_text(0), "0")
        payload = workspaces.build_windows_payload([
            {"index": 0, "name": "First", "current": True, "apps": []},
        ])
        self.assertEqual(payload["workspaces"][0]["id"], "0")


if __name__ == "__main__":
    unittest.main()
