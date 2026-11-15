package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"unicode/utf8"
)

func hyprClientFor(workspace int, class, title string) hyprClient {
	var client hyprClient
	client.Workspace.ID = workspace
	client.Class = class
	client.Title = title
	return client
}

func TestBuildHyprland(t *testing.T) {
	snapshot := buildHyprland(
		[]hyprWorkspace{
			{ID: 1, Name: "1", Monitor: "DP-1", Windows: 2, LastWindowTitle: "orqella - Visual Studio Code"},
			{ID: -98, Name: "special:scratchpad", Monitor: "DP-1", Windows: 1},
		},
		[]hyprClient{
			hyprClientFor(1, "com.microsoft.VSCode", "orqella - Visual Studio Code"),
			hyprClientFor(1, "foot", "eolu@host:~/Projects"),
			hyprClientFor(-98, "chrome-orqella.localhost__-Default", "Orqella"),
		},
		hyprActive{ID: 1, Name: "1"},
	)
	if !snapshot.Available || snapshot.Backend != "hyprland" || snapshot.Current != "1" {
		t.Fatalf("unexpected payload: %+v", snapshot)
	}
	if !snapshot.Workspaces[0].Active || snapshot.Workspaces[0].Apps[0].Name != "VSCode" {
		t.Fatalf("unexpected first workspace: %+v", snapshot.Workspaces[0])
	}
	if snapshot.Workspaces[1].Kind != "special" || snapshot.Workspaces[1].Label != "Scratchpad" {
		t.Fatalf("unexpected special workspace: %+v", snapshot.Workspaces[1])
	}
}

func TestBuildI3(t *testing.T) {
	tree := map[string]any{
		"type": "root",
		"nodes": []any{
			map[string]any{
				"type": "workspace",
				"name": "1: web",
				"nodes": []any{
					map[string]any{
						"type":              "con",
						"name":              "Docs - Chromium",
						"window":            float64(123),
						"window_properties": map[string]any{"class": "chromium"},
					},
				},
			},
		},
	}
	snapshot := buildI3([]i3Workspace{
		{Name: "1: web", Num: 1, Output: "eDP-1", Focused: true},
		{Name: "2", Num: 2, Output: "eDP-1"},
	}, tree, "Sway")
	if snapshot.Backend != "sway" || snapshot.Current != "1: web" || snapshot.Workspaces[0].Windows != 1 {
		t.Fatalf("unexpected payload: %+v", snapshot)
	}
}

func TestBuildWmctrl(t *testing.T) {
	snapshot := buildWmctrl(
		"0  * DG: 1920x1080  VP: 0,0  WA: 0,0 1920,1080  Work\n"+
			"1  - DG: 1920x1080  VP: 0,0  WA: 0,0 1920,1080  N/A\n",
		"0x04600007 0 host code.Code Project - Visual Studio Code\n"+
			"0x04600008 -1 host sticky.Sticky Sticky window\n",
		[]string{"Work", "Play"},
	)
	if snapshot.Backend != "wmctrl" || snapshot.Current != "0" {
		t.Fatalf("unexpected payload: %+v", snapshot)
	}
	if snapshot.Workspaces[0].Label != "Work" || snapshot.Workspaces[1].Label != "Play" {
		t.Fatalf("unexpected labels: %+v", snapshot.Workspaces)
	}
	if snapshot.Workspaces[0].Apps[0].Name != "Code" {
		t.Fatalf("unexpected apps: %+v", snapshot.Workspaces[0].Apps)
	}
}

func TestBuildAeroSpace(t *testing.T) {
	snapshot := buildAeroSpace(
		"1\ttrue\tfalse\t1\tBuilt-in\n"+
			"2\tfalse\tfalse\t1\tBuilt-in\n"+
			"M\tfalse\ttrue\t2\tStudio\n",
		"1\tTerminal\t~/Projects\n"+
			"M\tSafari\tDocs\n",
	)
	if snapshot.Backend != "aerospace" || snapshot.Current != "1" {
		t.Fatalf("unexpected payload: %+v", snapshot)
	}
	if len(snapshot.Workspaces) != 3 || snapshot.Workspaces[0].Windows != 1 {
		t.Fatalf("unexpected workspaces: %+v", snapshot.Workspaces)
	}
	if snapshot.Workspaces[2].Monitor != "Studio" || snapshot.Workspaces[2].Active {
		t.Fatalf("unexpected visible workspace: %+v", snapshot.Workspaces[2])
	}
}

func TestBuildWindows(t *testing.T) {
	snapshot := buildWindows([]winDesktop{
		{Index: 0, Name: "Main", Current: true, Apps: []winApp{{Name: "Code", Title: "Project"}}},
		{Index: 1, Name: "", Apps: []winApp{}},
	})
	if snapshot.Backend != "windows" || snapshot.Current != "0" {
		t.Fatalf("unexpected payload: %+v", snapshot)
	}
	if snapshot.Workspaces[0].Label != "Main" || snapshot.Workspaces[1].Label != "Desktop 2" {
		t.Fatalf("unexpected labels: %+v", snapshot.Workspaces)
	}
}

func TestBuildKDEAndYabai(t *testing.T) {
	kde := buildKDE(4, 2)
	if len(kde.Workspaces) != 4 || kde.Workspaces[1].Label != "Desktop 2" || !kde.Workspaces[1].Active {
		t.Fatalf("unexpected KDE payload: %+v", kde)
	}
	zeroBased := buildKDE(3, 0)
	if zeroBased.Workspaces[0].ID != "0" || !zeroBased.Workspaces[0].Active {
		t.Fatalf("unexpected zero-based KDE payload: %+v", zeroBased)
	}
	yabai := buildYabai(
		[]yabaiSpace{{Index: 1, Label: "Term", Display: 1, HasFocus: true}, {Index: 2, Display: 1}},
		[]yabaiWindow{{Space: 1, App: "WezTerm", Title: "shell"}, {Space: 2, App: "Safari", Title: "Docs"}},
	)
	if yabai.Workspaces[0].Label != "Term" || yabai.Workspaces[1].Label != "Space 2" {
		t.Fatalf("unexpected yabai payload: %+v", yabai)
	}
}

func TestBundledWindowsHelperIsPresent(t *testing.T) {
	for _, name := range []string{"VirtualDesktop.psd1", "functions.cat", "LICENSE"} {
		if _, err := os.Stat(filepath.Join("vendor", "psvirtualdesktop", name)); err != nil {
			t.Fatalf("missing vendored helper %s: %v", name, err)
		}
	}
}

func TestSafeTextKeepsZero(t *testing.T) {
	if safeText("0", 10) != "0" {
		t.Fatal("zero identifier was lost")
	}
}

func TestSafeTextPreservesValidUTF8(t *testing.T) {
	truncated := safeText("🚀🔥✨", 2)
	if !utf8.ValidString(truncated) {
		t.Fatalf("safeText produced invalid UTF-8: %q", truncated)
	}
	if truncated != "🚀🔥" {
		t.Fatalf("unexpected truncated value: %q", truncated)
	}
}

func TestSafeTextRepairsInvalidUTF8(t *testing.T) {
	value := safeText(string([]byte{0xff, 'a', 'b'}), 10)
	if !utf8.ValidString(value) {
		t.Fatalf("safeText produced invalid UTF-8: %q", value)
	}
}

func TestFilterEnvRemovesModuleCredentials(t *testing.T) {
	clean := filterEnv([]string{
		"ORQELLA_MODULE_TOKEN=secret",
		"ORQELLA_MODULE_CONFIG={}",
		"KEEP=yes",
	})
	if len(clean) != 1 || clean[0] != "KEEP=yes" {
		t.Fatalf("unexpected env: %+v", clean)
	}
}

func TestServerRejectsMissingTokenAndPostSnapshot(t *testing.T) {
	state := &serverState{token: "expected"}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/workspaces", nil)
	if state.authorized(recorder, request) {
		t.Fatal("missing token was accepted")
	}
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("unexpected status: %d", recorder.Code)
	}

	recorder = httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodPost, "/api/workspaces", nil)
	request.Header.Set("X-Orqella-Module-Token", "expected")
	state.snapshot(recorder, request)
	if recorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("unexpected status: %d", recorder.Code)
	}
}

func TestI3ActivateRejectsCommandSeparators(t *testing.T) {
	backend := &i3Backend{binary: "/bin/echo"}
	for _, name := range []string{"1; exec xterm", "1\nworkspace 2", "1\rworkspace 2"} {
		if err := backend.activate(Workspace{Name: name}); err == nil {
			t.Fatalf("expected workspace name %q to be rejected", name)
		}
	}
}
