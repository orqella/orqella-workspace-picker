"""Workspace picker sidecar.

The daemon starts this process with ORQELLA_MODULE_PORT and
ORQELLA_MODULE_TOKEN. Keep the API small: one snapshot endpoint and one
activation endpoint. Desktop-specific command handling lives in workspaces.py.
"""
from __future__ import annotations

import json
import os
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.parse import urlsplit

from workspaces import WorkspaceError, WorkspaceManager

TOKEN = os.environ.get("ORQELLA_MODULE_TOKEN", "")
PORT = int(os.environ.get("ORQELLA_MODULE_PORT") or "0")
MANAGER = WorkspaceManager()


class Handler(BaseHTTPRequestHandler):
    def log_message(self, *args):
        pass

    def send_json(self, status: int, body: dict) -> None:
        data = json.dumps(body, separators=(",", ":")).encode("utf-8")
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(data)))
        self.send_header("Cache-Control", "no-store")
        self.send_header("X-Content-Type-Options", "nosniff")
        self.end_headers()
        self.wfile.write(data)

    def authorized(self) -> bool:
        if TOKEN and self.headers.get("X-Orqella-Module-Token") == TOKEN:
            return True
        self.send_json(401, {"ok": False, "error": "unauthorized"})
        return False

    def read_json(self) -> dict:
        length = int(self.headers.get("Content-Length") or 0)
        if length <= 0:
            return {}
        if length > 65536:
            raise WorkspaceError("Request body is too large")
        payload = json.loads(self.rfile.read(length) or b"{}")
        return payload if isinstance(payload, dict) else {}

    def do_GET(self) -> None:
        if not self.authorized():
            return
        path = urlsplit(self.path).path
        if path == "/health":
            return self.send_json(200, {"ok": True})
        if path == "/api/workspaces":
            return self.send_json(200, MANAGER.snapshot())
        return self.send_json(404, {"ok": False, "error": "not found"})

    def do_POST(self) -> None:
        if not self.authorized():
            return
        path = urlsplit(self.path).path
        if path != "/api/workspaces/activate":
            return self.send_json(404, {"ok": False, "error": "not found"})
        try:
            payload = self.read_json()
            workspace_id = payload.get("id")
            if not isinstance(workspace_id, str) or not workspace_id:
                return self.send_json(400, {"ok": False, "error": "workspace id is required"})
            return self.send_json(200, MANAGER.activate(workspace_id))
        except WorkspaceError as error:
            return self.send_json(400, {"ok": False, "error": str(error)})
        except (OSError, ValueError, json.JSONDecodeError) as error:
            return self.send_json(400, {"ok": False, "error": f"Invalid request: {error}"})


def main() -> None:
    if not TOKEN:
        raise SystemExit("ORQELLA_MODULE_TOKEN is required")
    if PORT <= 0:
        raise SystemExit("ORQELLA_MODULE_PORT is required")
    server = ThreadingHTTPServer(("127.0.0.1", PORT), Handler)
    server.serve_forever()


if __name__ == "__main__":
    main()
