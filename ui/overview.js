(() => {
  "use strict";

  const $ = (id) => document.getElementById(id);
  const esc = (value) => String(value ?? "").replace(/[&<>"']/g, (character) => ({
    "&": "&amp;",
    "<": "&lt;",
    ">": "&gt;",
    '"': "&quot;",
    "'": "&#39;",
  }[character]));

  const REFRESH_MS = 8000;
  let context = null;
  let timer = null;
  let loading = false;
  let busy = false;
  let requestSequence = 0;

  const request = (method, path, body) => window.orqella.call("backend.request", {
    method,
    path,
    body,
  });

  function setStatus(text, bad = false) {
    const status = $("workspaceStatus");
    status.textContent = text || "";
    status.className = bad ? "bad" : "";
  }

  function setRefreshBusy(value) {
    const button = $("refresh");
    button.classList.toggle("is-loading", value);
    button.disabled = value || busy;
  }

  function setRowsDisabled(value) {
    $("workspaceList").querySelectorAll("[data-workspace]").forEach((button) => {
      button.disabled = value;
    });
    $("refresh").disabled = value || loading;
  }

  function workspaceApps(workspace) {
    const apps = Array.isArray(workspace.apps) ? workspace.apps : [];
    if (apps.length) {
      const visible = apps.slice(0, 3).map((app) => {
        const name = app?.name || "App";
        const title = app?.title || name;
        return `<span class="workspace-app" title="${esc(title)}">${esc(name)}</span>`;
      }).join("");
      const extra = apps.length > 3
        ? `<span class="workspace-app more">+${apps.length - 3}</span>`
        : "";
      return visible + extra;
    }
    if (workspace.preview) {
      return `<span class="workspace-app" title="${esc(workspace.preview)}">${esc(workspace.preview)}</span>`;
    }
    return '<span class="workspace-app none">No windows</span>';
  }

  function renderWorkspace(workspace) {
    const label = workspace.label || workspace.name || workspace.id;
    const monitor = workspace.monitor
      ? `<span class="workspace-monitor">${esc(workspace.monitor)}</span>`
      : "";
    const current = workspace.active
      ? '<span class="workspace-current">current</span>'
      : "";
    const count = Number.isFinite(Number(workspace.windows)) ? Number(workspace.windows) : 0;
    const apps = Array.isArray(workspace.apps) ? workspace.apps : [];
    const description = [
      `Workspace ${label}`,
      workspace.monitor || "",
      count === 1 ? "1 window" : `${count} windows`,
      apps.map((app) => app?.name).filter(Boolean).join(", "),
    ].filter(Boolean).join(" · ");
    return `<button class="workspace-item${workspace.active ? " active" : ""}" type="button" data-workspace="${esc(workspace.id)}" aria-pressed="${workspace.active ? "true" : "false"}" aria-label="${esc(description)}" title="${esc(description)}">
      <span class="workspace-copy">
        <span class="workspace-line"><b>${esc(label)}</b>${monitor}${current}</span>
        <span class="workspace-apps">${workspaceApps(workspace)}</span>
      </span>
      <span class="workspace-windows"><b>${count}</b><span>${count === 1 ? "win" : "wins"}</span></span>
    </button>`;
  }

  function currentLabel(data) {
    return (data.workspaces || []).find((workspace) => workspace.active)?.label || "";
  }

  function render(data) {
    const list = $("workspaceList");
    const workspaces = Array.isArray(data?.workspaces) ? data.workspaces : [];
    $("backendLabel").textContent = data?.desktop || "Workspace backend";
    list.setAttribute("aria-busy", "false");

    if (!data?.available) {
      const error = data?.error || "Workspace switching is not available here.";
      const hint = data?.hint ? `<small>${esc(data.hint)}</small>` : "";
      list.innerHTML = `<div class="workspace-empty"><b>${esc(error)}</b>${hint}</div>`;
      setStatus("unavailable", true);
      return;
    }

    if (!workspaces.length) {
      list.innerHTML = '<div class="workspace-empty"><b>No open workspaces</b><small>This desktop did not report any workspaces.</small></div>';
      setStatus("0 workspaces");
      return;
    }

    list.innerHTML = workspaces.map(renderWorkspace).join("");
    list.querySelectorAll("[data-workspace]").forEach((button) => {
      button.onclick = () => activate(button.dataset.workspace);
    });
    const current = currentLabel(data);
    const count = workspaces.length;
    setStatus(current ? `current · ${current}` : `${count} workspace${count === 1 ? "" : "s"}`);
  }

  function renderError(error) {
    $("backendLabel").textContent = "Workspace backend";
    $("workspaceList").setAttribute("aria-busy", "false");
    $("workspaceList").innerHTML = `<div class="workspace-empty"><b>Workspace backend unavailable</b><small>${esc(error?.message || error || "Could not read workspaces.")}</small></div>`;
    setStatus("unavailable", true);
  }

  async function load(force = false) {
    if (loading || (busy && !force)) return;
    loading = true;
    setRefreshBusy(true);
    $("workspaceList").setAttribute("aria-busy", "true");
    const sequence = ++requestSequence;
    try {
      const data = await request("GET", "/api/workspaces");
      if (sequence !== requestSequence) return;
      render(data);
    } catch (error) {
      if (sequence === requestSequence) renderError(error);
    } finally {
      loading = false;
      setRefreshBusy(false);
    }
  }

  async function activate(workspaceId) {
    if (!workspaceId || busy) return;
    const workspace = [...$("workspaceList").querySelectorAll("[data-workspace]")]
      .find((button) => button.dataset.workspace === workspaceId);
    const label = workspace?.querySelector(".workspace-line b")?.textContent || "workspace";
    busy = true;
    setRowsDisabled(true);
    setStatus(`switching to ${label}…`);
    try {
      const result = await request("POST", "/api/workspaces/activate", { id: workspaceId });
      if (!result?.ok) throw new Error(result?.error || "Could not switch workspace.");
      await load(true);
    } catch (error) {
      setStatus(error.message || "Could not switch workspace", true);
    } finally {
      busy = false;
      setRowsDisabled(false);
    }
  }

  window.orqella.ready((readyContext) => {
    context = readyContext || {};
    const name = context.node?.name || "This machine";
    $("machineLabel").textContent = name;
    $("refresh").onclick = () => load();
    load();
    timer = window.setInterval(() => {
      if (!document.hidden) load();
    }, REFRESH_MS);
    document.addEventListener("visibilitychange", () => {
      if (!document.hidden) load();
    });
    window.addEventListener("pagehide", () => {
      if (timer) window.clearInterval(timer);
    });
  });
})();
