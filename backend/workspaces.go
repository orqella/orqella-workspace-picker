package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
)

type App struct {
	Name  string `json:"name"`
	Title string `json:"title"`
}

type Workspace struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Label   string `json:"label"`
	Monitor string `json:"monitor"`
	Active  bool   `json:"active"`
	Windows int    `json:"windows"`
	Apps    []App  `json:"apps"`
	Kind    string `json:"kind"`
	Number  *int   `json:"number"`
	Preview string `json:"preview"`
}

type Snapshot struct {
	OK         bool        `json:"ok"`
	Available  bool        `json:"available"`
	Backend    string      `json:"backend"`
	Desktop    string      `json:"desktop"`
	Platform   string      `json:"platform"`
	Current    string      `json:"current"`
	Workspaces []Workspace `json:"workspaces"`
	Error      string      `json:"error"`
	Hint       string      `json:"hint"`
	RefreshMS  int         `json:"refresh_ms"`
}

type activation struct {
	OK        bool      `json:"ok"`
	Available bool      `json:"available"`
	Workspace Workspace `json:"workspace,omitempty"`
	Error     string    `json:"error,omitempty"`
	Hint      string    `json:"hint,omitempty"`
}

type workspaceBackend interface {
	snapshot() (Snapshot, error)
	activate(Workspace) error
}

type appBucket struct {
	windows int
	apps    []App
	seen    map[string]bool
}

func newBucket() *appBucket {
	return &appBucket{apps: []App{}, seen: map[string]bool{}}
}

func bucketFor(groups map[string]*appBucket, key string) *appBucket {
	if group, ok := groups[key]; ok {
		return group
	}
	group := newBucket()
	groups[key] = group
	return group
}

func hostHome() string {
	if value := strings.TrimSpace(os.Getenv("ORQELLA_HOST_HOME")); value != "" {
		return value
	}
	home, _ := os.UserHomeDir()
	return home
}

func safeText(value string, limit int) string {
	value = strings.ReplaceAll(value, "\x00", "")
	value = strings.TrimSpace(value)
	if limit <= 0 {
		return ""
	}
	value = strings.ToValidUTF8(value, "")
	runes := []rune(value)
	if len(runes) > limit {
		value = string(runes[:limit])
	}
	return value
}

func appName(identifier, title string) string {
	ident := safeText(identifier, 100)
	title = safeText(title, 160)
	if cut := strings.Index(ident, ".desktop"); cut >= 0 {
		ident = ident[:cut]
	}
	if ident != "" {
		low := strings.ToLower(ident)
		if strings.HasPrefix(low, "chrome-") || strings.HasPrefix(low, "chromium-") ||
			strings.HasPrefix(low, "firefox-") || strings.HasPrefix(low, "brave-") {
			ident = firstTitlePart(title)
		} else if strings.Count(ident, ".") >= 2 {
			prefix := strings.ToLower(strings.SplitN(ident, ".", 2)[0])
			switch prefix {
			case "app", "com", "dev", "io", "net", "org":
				parts := strings.Split(ident, ".")
				ident = parts[len(parts)-1]
			}
		}
		ident = strings.ReplaceAll(ident, "__", " ")
		if ident = strings.TrimSpace(ident); ident != "" {
			return safeText(ident, 48)
		}
	}
	if title != "" {
		return safeText(firstTitlePart(title), 48)
	}
	return "App"
}

func firstTitlePart(title string) string {
	if cut := strings.Index(title, " - "); cut >= 0 {
		return title[:cut]
	}
	return title
}

func addApp(group *appBucket, identifier, title string) {
	entry := App{Name: appName(identifier, title), Title: safeText(title, 160)}
	key := strings.ToLower(entry.Name)
	if key == "" || group.seen[key] {
		return
	}
	group.seen[key] = true
	group.apps = append(group.apps, entry)
}

func intPtr(value int) *int {
	return &value
}

func makeWorkspace(id, name, label, monitor string, active bool, windows int, apps []App, kind string, number *int, preview string) Workspace {
	if apps == nil {
		apps = []App{}
	}
	return Workspace{
		ID:      safeText(id, 100),
		Name:    safeText(name, 100),
		Label:   safeText(label, 100),
		Monitor: safeText(monitor, 80),
		Active:  active,
		Windows: max(0, windows),
		Apps:    apps,
		Kind:    kind,
		Number:  number,
		Preview: safeText(preview, 160),
	}
}

func sortWorkspaces(workspaces []Workspace) {
	sort.SliceStable(workspaces, func(i, j int) bool {
		left, right := workspaces[i], workspaces[j]
		if left.Kind != right.Kind {
			return left.Kind != "special"
		}
		leftNumber, rightNumber := 1<<30, 1<<30
		if left.Number != nil {
			leftNumber = *left.Number
		}
		if right.Number != nil {
			rightNumber = *right.Number
		}
		if leftNumber != rightNumber {
			return leftNumber < rightNumber
		}
		return strings.ToLower(left.Label) < strings.ToLower(right.Label)
	})
}

func platformName() string {
	switch runtime.GOOS {
	case "darwin":
		return "macos"
	case "windows":
		return "windows"
	default:
		return runtime.GOOS
	}
}

func desktopName() string {
	for _, key := range []string{"XDG_CURRENT_DESKTOP", "DESKTOP_SESSION"} {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			if cut := strings.Index(value, ":"); cut >= 0 {
				value = value[:cut]
			}
			if value != "" {
				return safeText(value, 80)
			}
		}
	}
	return platformName()
}

func unsupported(message, hint string) Snapshot {
	return Snapshot{
		OK:         true,
		Available:  false,
		Backend:    "unsupported",
		Desktop:    desktopName(),
		Platform:   platformName(),
		Workspaces: []Workspace{},
		Error:      safeText(message, 500),
		Hint:       safeText(hint, 500),
		RefreshMS:  refreshMS(),
	}
}

func refreshMS() int {
	if runtime.GOOS == "windows" {
		return 15000
	}
	return 8000
}

func payload(backend, desktop string, workspaces []Workspace, current string) Snapshot {
	sortWorkspaces(workspaces)
	return Snapshot{
		OK:         true,
		Available:  true,
		Backend:    backend,
		Desktop:    desktop,
		Platform:   platformName(),
		Current:    safeText(current, 100),
		Workspaces: workspaces,
		RefreshMS:  refreshMS(),
	}
}

func unsupportedHint() string {
	switch runtime.GOOS {
	case "darwin":
		return "Install AeroSpace (recommended) or yabai to expose macOS workspaces."
	case "windows":
		return "Run Orqella in the logged-in desktop session. A session-0 service or SSH process cannot see Windows virtual desktops."
	default:
		return "Supported Linux controllers: Hyprland, Sway, i3, KDE Plasma, or any EWMH X11 window manager with wmctrl."
	}
}

func runCommand(name string, args []string, env []string, timeout time.Duration) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	command := exec.CommandContext(ctx, name, args...)
	if env != nil {
		command.Env = env
	}
	output, err := command.CombinedOutput()
	text := strings.TrimSpace(string(output))
	if err != nil {
		if text == "" {
			text = err.Error()
		}
		if len(text) > 500 {
			text = text[len(text)-500:]
		}
		return "", fmt.Errorf("%s", text)
	}
	return string(output), nil
}

func childEnv() []string {
	env := filterEnv(os.Environ())
	if runtime.GOOS == "darwin" {
		path := os.Getenv("PATH")
		prefixes := []string{"/opt/homebrew/bin", "/usr/local/bin"}
		if home := hostHome(); home != "" {
			prefixes = append(prefixes, filepath.Join(home, "bin"), filepath.Join(home, ".cargo", "bin"))
		}
		parts := append(prefixes, path)
		env = setEnv(env, "PATH", strings.Join(parts, ":"))
	}
	return env
}

func filterEnv(env []string) []string {
	clean := make([]string, 0, len(env))
	for _, item := range env {
		if strings.HasPrefix(item, "ORQELLA_MODULE_TOKEN=") || strings.HasPrefix(item, "ORQELLA_MODULE_CONFIG=") {
			continue
		}
		clean = append(clean, item)
	}
	return clean
}

func setEnv(env []string, key, value string) []string {
	prefix := key + "="
	for index, item := range env {
		if strings.HasPrefix(item, prefix) {
			env[index] = prefix + value
			return env
		}
	}
	return append(env, prefix+value)
}

func lookPath(name string) string {
	if runtime.GOOS == "darwin" {
		candidates := []string{"/opt/homebrew/bin/" + name, "/usr/local/bin/" + name}
		if home := hostHome(); home != "" {
			candidates = append(candidates, filepath.Join(home, "bin", name), filepath.Join(home, ".cargo", "bin", name))
		}
		for _, candidate := range candidates {
			if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
				return candidate
			}
		}
	}
	if path, err := exec.LookPath(name); err == nil {
		return path
	}
	return ""
}

func runtimeDir() string {
	if value := os.Getenv("XDG_RUNTIME_DIR"); value != "" {
		return value
	}
	if runtime.GOOS == "linux" {
		return fmt.Sprintf("/run/user/%d", currentUID())
	}
	return os.TempDir()
}

func hyprlandEnvs() [][]string {
	base := childEnv()
	runtime := runtimeDir()
	base = setEnv(base, "XDG_RUNTIME_DIR", runtime)
	variants := [][]string{}
	seen := map[string]bool{}
	add := func(env []string) {
		signature := envValue(env, "HYPRLAND_INSTANCE_SIGNATURE")
		if seen[signature] {
			return
		}
		seen[signature] = true
		variants = append(variants, env)
	}
	if envValue(base, "HYPRLAND_INSTANCE_SIGNATURE") != "" {
		add(cloneEnv(base))
	}
	entries, _ := filepath.Glob(filepath.Join(runtime, "hypr", "*"))
	sort.Slice(entries, func(i, j int) bool {
		left, leftErr := os.Stat(entries[i])
		right, rightErr := os.Stat(entries[j])
		if leftErr != nil || rightErr != nil {
			return entries[i] < entries[j]
		}
		return left.ModTime().After(right.ModTime())
	})
	for _, path := range entries {
		if _, err := os.Stat(filepath.Join(path, ".socket.sock")); err != nil {
			continue
		}
		env := cloneEnv(base)
		env = setEnv(env, "HYPRLAND_INSTANCE_SIGNATURE", filepath.Base(path))
		add(env)
	}
	if len(variants) == 0 {
		variants = append(variants, base)
	}
	return variants
}

func socketEnv(variable, pattern string) []string {
	env := childEnv()
	if envValue(env, variable) != "" {
		return env
	}
	resolved := runtimeDir()
	matches, _ := filepath.Glob(filepath.Join(resolved, pattern))
	if len(matches) > 0 {
		sort.Strings(matches)
		env = setEnv(env, variable, matches[len(matches)-1])
	}
	return env
}

func cloneEnv(env []string) []string {
	return append([]string(nil), env...)
}

func envValue(env []string, key string) string {
	prefix := key + "="
	for _, item := range env {
		if strings.HasPrefix(item, prefix) {
			return strings.TrimPrefix(item, prefix)
		}
	}
	return ""
}

type hyprWorkspace struct {
	ID              int    `json:"id"`
	Name            string `json:"name"`
	Monitor         string `json:"monitor"`
	Windows         int    `json:"windows"`
	LastWindowTitle string `json:"lastwindowtitle"`
}

type hyprClient struct {
	Workspace struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	} `json:"workspace"`
	Class        string `json:"class"`
	InitialClass string `json:"initialClass"`
	Title        string `json:"title"`
}

type hyprActive struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type hyprlandBackend struct {
	env []string
}

func detectHyprland() workspaceBackend {
	if lookPath("hyprctl") == "" {
		return nil
	}
	for _, env := range hyprlandEnvs() {
		if _, err := runCommand("hyprctl", []string{"-j", "activeworkspace"}, env, 2*time.Second); err == nil {
			return &hyprlandBackend{env: env}
		}
	}
	return nil
}

func buildHyprland(workspaces []hyprWorkspace, clients []hyprClient, active hyprActive) Snapshot {
	groups := map[string]*appBucket{}
	for _, client := range clients {
		key := strconv.Itoa(client.Workspace.ID)
		group := bucketFor(groups, key)
		group.windows++
		identifier := client.Class
		if identifier == "" {
			identifier = client.InitialClass
		}
		addApp(group, identifier, client.Title)
	}
	workspaceList := make([]Workspace, 0, len(workspaces))
	activeID := strconv.Itoa(active.ID)
	for _, item := range workspaces {
		rawName := item.Name
		if rawName == "" {
			rawName = strconv.Itoa(item.ID)
		}
		special := item.ID < 0 || strings.HasPrefix(rawName, "special:")
		label := rawName
		if special {
			if rawName == "special:scratchpad" {
				label = "Scratchpad"
			} else if cut := strings.Index(rawName, ":"); cut >= 0 {
				label = rawName[cut+1:]
			}
			if label == "" {
				label = "Special"
			}
		}
		group := groups[strconv.Itoa(item.ID)]
		if group == nil {
			group = newBucket()
		}
		windows := item.Windows
		if windows == 0 {
			windows = group.windows
		}
		var number *int
		if item.ID >= 0 {
			number = intPtr(item.ID)
		}
		kind := "workspace"
		if special {
			kind = "special"
		}
		workspaceList = append(workspaceList, makeWorkspace(
			strconv.Itoa(item.ID), rawName, label, item.Monitor,
			strconv.Itoa(item.ID) == activeID, windows, group.apps, kind, number,
			item.LastWindowTitle,
		))
	}
	return payload("hyprland", "Hyprland", workspaceList, activeID)
}

func (backend *hyprlandBackend) snapshot() (Snapshot, error) {
	workspaceOutput, err := runCommand("hyprctl", []string{"-j", "workspaces"}, backend.env, 3*time.Second)
	if err != nil {
		return Snapshot{}, err
	}
	clientOutput, err := runCommand("hyprctl", []string{"-j", "clients"}, backend.env, 3*time.Second)
	if err != nil {
		return Snapshot{}, err
	}
	activeOutput, err := runCommand("hyprctl", []string{"-j", "activeworkspace"}, backend.env, 3*time.Second)
	if err != nil {
		return Snapshot{}, err
	}
	var workspaces []hyprWorkspace
	var clients []hyprClient
	var active hyprActive
	if err := json.Unmarshal([]byte(workspaceOutput), &workspaces); err != nil {
		return Snapshot{}, fmt.Errorf("could not parse Hyprland workspaces: %w", err)
	}
	if err := json.Unmarshal([]byte(clientOutput), &clients); err != nil {
		return Snapshot{}, fmt.Errorf("could not parse Hyprland clients: %w", err)
	}
	if err := json.Unmarshal([]byte(activeOutput), &active); err != nil {
		return Snapshot{}, fmt.Errorf("could not parse Hyprland active workspace: %w", err)
	}
	return buildHyprland(workspaces, clients, active), nil
}

func luaString(value string) string {
	value = strings.ReplaceAll(value, "\\", "\\\\")
	value = strings.ReplaceAll(value, "\"", "\\\"")
	value = strings.ReplaceAll(value, "\n", "\\n")
	value = strings.ReplaceAll(value, "\r", "\\r")
	value = strings.ReplaceAll(value, "\t", "\\t")
	return "\"" + value + "\""
}

func (backend *hyprlandBackend) activate(workspace Workspace) error {
	name := workspace.Name
	if name == "" {
		name = workspace.ID
	}
	var modern string
	var legacy []string
	if workspace.Kind == "special" {
		special := name
		if cut := strings.Index(special, ":"); cut >= 0 {
			special = special[cut+1:]
		}
		modern = "hl.dsp.workspace.toggle_special(" + luaString(special) + ")"
		legacy = []string{"hyprctl", "dispatch", "togglespecialworkspace", special}
	} else if _, err := strconv.Atoi(workspace.ID); err == nil {
		modern = fmt.Sprintf("hl.dsp.focus({ workspace = %s })", workspace.ID)
		legacy = []string{"hyprctl", "dispatch", "workspace", workspace.ID}
	} else {
		modern = fmt.Sprintf("hl.dsp.focus({ workspace = %s })", luaString(name))
		legacy = []string{"hyprctl", "dispatch", "workspace", "name:" + name}
	}
	if _, err := runCommand("hyprctl", []string{"dispatch", modern}, backend.env, 3*time.Second); err == nil {
		return nil
	}
	_, err := runCommand(legacy[0], legacy[1:], backend.env, 3*time.Second)
	return err
}

type i3Workspace struct {
	Name    string `json:"name"`
	Num     int    `json:"num"`
	Output  string `json:"output"`
	Focused bool   `json:"focused"`
}

type i3Backend struct {
	binary string
	raw    bool
	env    []string
	key    string
	label  string
}

func detectSway() workspaceBackend {
	if lookPath("swaymsg") == "" {
		return nil
	}
	env := socketEnv("SWAYSOCK", "sway-ipc.*.sock")
	if _, err := runCommand("swaymsg", []string{"-r", "-t", "get_workspaces"}, env, 2*time.Second); err != nil {
		return nil
	}
	return &i3Backend{binary: "swaymsg", raw: true, env: env, key: "sway", label: "Sway"}
}

func detectI3() workspaceBackend {
	if lookPath("i3-msg") == "" {
		return nil
	}
	env := childEnv()
	if _, err := runCommand("i3-msg", []string{"-t", "get_workspaces"}, env, 2*time.Second); err != nil {
		return nil
	}
	return &i3Backend{binary: "i3-msg", env: env, key: "i3", label: "i3"}
}

func (backend *i3Backend) query(kind string) (string, error) {
	args := []string{}
	if backend.raw {
		args = append(args, "-r")
	}
	args = append(args, "-t", kind)
	return runCommand(backend.binary, args, backend.env, 3*time.Second)
}

func walkI3(node any, workspace string, groups map[string]*appBucket) {
	object, ok := node.(map[string]any)
	if !ok {
		return
	}
	if value, ok := object["type"].(string); ok && value == "workspace" {
		workspace = safeText(fmt.Sprint(object["name"]), 100)
		if workspace == "" {
			workspace = safeText(fmt.Sprint(object["num"]), 100)
		}
	}
	if workspace != "" {
		hasWindow := object["window"] != nil
		appID, _ := object["app_id"].(string)
		properties, _ := object["window_properties"].(map[string]any)
		if hasWindow || appID != "" || properties != nil {
			group := bucketFor(groups, workspace)
			group.windows++
			identifier := appID
			if identifier == "" && properties != nil {
				identifier, _ = properties["class"].(string)
				if identifier == "" {
					identifier, _ = properties["instance"].(string)
				}
			}
			title, _ := object["name"].(string)
			if title == "" && properties != nil {
				title, _ = properties["title"].(string)
			}
			addApp(group, identifier, title)
		}
	}
	for _, key := range []string{"nodes", "floating_nodes"} {
		children, _ := object[key].([]any)
		for _, child := range children {
			walkI3(child, workspace, groups)
		}
	}
}

func buildI3(workspaces []i3Workspace, tree any, label string) Snapshot {
	groups := map[string]*appBucket{}
	walkI3(tree, "", groups)
	list := make([]Workspace, 0, len(workspaces))
	current := ""
	for _, item := range workspaces {
		name := item.Name
		if name == "" {
			name = strconv.Itoa(item.Num)
		}
		group := groups[name]
		if group == nil {
			group = newBucket()
		}
		if item.Focused {
			current = name
		}
		var number *int
		if item.Num > 0 {
			number = intPtr(item.Num)
		}
		list = append(list, makeWorkspace(
			name, name, name, item.Output, item.Focused, group.windows, group.apps,
			"workspace", number, "",
		))
	}
	key := "sway"
	if label == "i3" {
		key = "i3"
	}
	return payload(key, label, list, current)
}

func (backend *i3Backend) snapshot() (Snapshot, error) {
	workspaceOutput, err := backend.query("get_workspaces")
	if err != nil {
		return Snapshot{}, err
	}
	treeOutput, err := backend.query("get_tree")
	if err != nil {
		return Snapshot{}, err
	}
	var workspaces []i3Workspace
	var tree any
	if err := json.Unmarshal([]byte(workspaceOutput), &workspaces); err != nil {
		return Snapshot{}, fmt.Errorf("could not parse %s workspaces: %w", backend.label, err)
	}
	if err := json.Unmarshal([]byte(treeOutput), &tree); err != nil {
		return Snapshot{}, fmt.Errorf("could not parse %s tree: %w", backend.label, err)
	}
	return buildI3(workspaces, tree, backend.label), nil
}

func (backend *i3Backend) activate(workspace Workspace) error {
	if workspace.Number != nil && *workspace.Number > 0 {
		_, err := runCommand(backend.binary, []string{"workspace", "number", strconv.Itoa(*workspace.Number)}, backend.env, 3*time.Second)
		return err
	}
	// i3-msg parses this argument as an i3 command; `;` and newlines are
	// command separators and must never come from a workspace name.
	if strings.ContainsAny(workspace.Name, ";\n\r") || strings.IndexFunc(workspace.Name, unicode.IsControl) >= 0 {
		return fmt.Errorf("workspace name contains unsupported characters")
	}
	_, err := runCommand(backend.binary, []string{"workspace", workspace.Name}, backend.env, 3*time.Second)
	return err
}

type wmctrlBackend struct{}

func detectWmctrl() workspaceBackend {
	if lookPath("wmctrl") == "" || os.Getenv("DISPLAY") == "" {
		return nil
	}
	if _, err := runCommand("wmctrl", []string{"-d"}, childEnv(), 2*time.Second); err != nil {
		return nil
	}
	return &wmctrlBackend{}
}

var wmctrlDesktopPattern = regexp.MustCompile(`^\s*(\d+)\s+([*-])\s+DG:\s*(\S+)\s+VP:\s+(\S+)\s+WA:\s+(\S+)\s+(\S+)\s*(.*)$`)

func x11Names() []string {
	if lookPath("xprop") == "" {
		return nil
	}
	output, err := runCommand("xprop", []string{"-root", "_NET_DESKTOP_NAMES"}, childEnv(), 2*time.Second)
	if err != nil {
		return nil
	}
	match := regexp.MustCompile(`=\s*(.*)`).FindStringSubmatch(output)
	if len(match) < 2 {
		return nil
	}
	quoted := regexp.MustCompile(`"((?:\\.|[^"\\])*)"`).FindAllStringSubmatch(match[1], -1)
	names := make([]string, 0, len(quoted))
	for _, item := range quoted {
		value := strings.ReplaceAll(item[1], `\"`, `"`)
		value = strings.ReplaceAll(value, `\\`, `\`)
		names = append(names, value)
	}
	return names
}

func buildWmctrl(desktopsText, windowsText string, names []string) Snapshot {
	groups := map[string]*appBucket{}
	for _, line := range strings.Split(windowsText, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 5 {
			continue
		}
		desktop, err := strconv.Atoi(fields[1])
		if err != nil || desktop < 0 {
			continue
		}
		group := bucketFor(groups, strconv.Itoa(desktop))
		group.windows++
		class := fields[3]
		if cut := strings.LastIndex(class, "."); cut >= 0 {
			class = class[cut+1:]
		}
		addApp(group, class, strings.Join(fields[4:], " "))
	}
	list := []Workspace{}
	current := ""
	for _, line := range strings.Split(desktopsText, "\n") {
		match := wmctrlDesktopPattern.FindStringSubmatch(line)
		if len(match) == 0 {
			continue
		}
		index, err := strconv.Atoi(match[1])
		if err != nil {
			continue
		}
		active := match[2] == "*"
		name := safeText(match[7], 100)
		if name == "" || strings.EqualFold(name, "N/A") {
			if index < len(names) {
				name = names[index]
			} else {
				name = fmt.Sprintf("Desktop %d", index+1)
			}
		}
		if active {
			current = strconv.Itoa(index)
		}
		group := groups[strconv.Itoa(index)]
		if group == nil {
			group = newBucket()
		}
		list = append(list, makeWorkspace(
			strconv.Itoa(index), name, name, "", active, group.windows, group.apps,
			"workspace", intPtr(index), "",
		))
	}
	return payload("wmctrl", "X11 / wmctrl", list, current)
}

func (backend *wmctrlBackend) snapshot() (Snapshot, error) {
	desktops, err := runCommand("wmctrl", []string{"-d"}, childEnv(), 3*time.Second)
	if err != nil {
		return Snapshot{}, err
	}
	windows, err := runCommand("wmctrl", []string{"-l", "-x"}, childEnv(), 3*time.Second)
	if err != nil {
		return Snapshot{}, err
	}
	return buildWmctrl(desktops, windows, x11Names()), nil
}

func (backend *wmctrlBackend) activate(workspace Workspace) error {
	if workspace.Number == nil || *workspace.Number < 0 {
		return fmt.Errorf("this X11 desktop does not have a valid index")
	}
	_, err := runCommand("wmctrl", []string{"-s", strconv.Itoa(*workspace.Number)}, childEnv(), 3*time.Second)
	return err
}

type kdeBackend struct{ command string }

func kwinInteger(command, method string) (int, error) {
	output, err := runCommand(command, []string{"org.kde.KWin", "/KWin", "org.kde.KWin." + method}, childEnv(), 2*time.Second)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(output))
}

func detectKDE() workspaceBackend {
	command := lookPath("qdbus6")
	if command == "" {
		command = lookPath("qdbus")
	}
	if command == "" {
		return nil
	}
	count, err := kwinInteger(command, "numberOfDesktops")
	if err != nil || count < 1 {
		return nil
	}
	if _, err := kwinInteger(command, "currentDesktop"); err != nil {
		return nil
	}
	return &kdeBackend{command: command}
}

func buildKDE(count, current int) Snapshot {
	if count < 1 {
		return Snapshot{}
	}
	oneBased := current >= 1
	list := make([]Workspace, 0, count)
	for index := 0; index < count; index++ {
		raw := index
		display := index + 1
		if oneBased {
			raw = index + 1
			display = raw
		}
		list = append(list, makeWorkspace(
			strconv.Itoa(raw), fmt.Sprintf("Desktop %d", display), fmt.Sprintf("Desktop %d", display),
			"", raw == current, 0, []App{}, "workspace", intPtr(display), "",
		))
	}
	return payload("kde", "KDE Plasma", list, strconv.Itoa(current))
}

func (backend *kdeBackend) snapshot() (Snapshot, error) {
	count, err := kwinInteger(backend.command, "numberOfDesktops")
	if err != nil {
		return Snapshot{}, err
	}
	current, err := kwinInteger(backend.command, "currentDesktop")
	if err != nil {
		return Snapshot{}, err
	}
	return buildKDE(count, current), nil
}

func (backend *kdeBackend) activate(workspace Workspace) error {
	index, err := strconv.Atoi(workspace.ID)
	if err != nil {
		return fmt.Errorf("this KDE desktop does not have a valid index")
	}
	_, err = runCommand(backend.command, []string{"org.kde.KWin", "/KWin", "org.kde.KWin.setCurrentDesktop", strconv.Itoa(index)}, childEnv(), 2*time.Second)
	return err
}

type yabaiSpace struct {
	Index    int    `json:"index"`
	Label    string `json:"label"`
	Display  int    `json:"display"`
	HasFocus bool   `json:"has-focus"`
}

type yabaiWindow struct {
	Space int    `json:"space"`
	App   string `json:"app"`
	Title string `json:"title"`
}

type yabaiBackend struct{ binary string }

func detectYabai() workspaceBackend {
	if runtime.GOOS != "darwin" {
		return nil
	}
	binary := lookPath("yabai")
	if binary == "" {
		return nil
	}
	if _, err := runCommand(binary, []string{"-m", "query", "--spaces"}, childEnv(), 3*time.Second); err != nil {
		return nil
	}
	return &yabaiBackend{binary: binary}
}

func buildYabai(spaces []yabaiSpace, windows []yabaiWindow) Snapshot {
	groups := map[string]*appBucket{}
	for _, window := range windows {
		group := bucketFor(groups, strconv.Itoa(window.Space))
		group.windows++
		addApp(group, window.App, window.Title)
	}
	list := make([]Workspace, 0, len(spaces))
	current := ""
	for _, space := range spaces {
		group := groups[strconv.Itoa(space.Index)]
		if group == nil {
			group = newBucket()
		}
		if space.HasFocus {
			current = strconv.Itoa(space.Index)
		}
		label := safeText(space.Label, 100)
		if label == "" {
			label = fmt.Sprintf("Space %d", space.Index)
		}
		monitor := ""
		if space.Display != 0 {
			monitor = fmt.Sprintf("Display %d", space.Display)
		}
		list = append(list, makeWorkspace(
			strconv.Itoa(space.Index), label, label, monitor, space.HasFocus,
			group.windows, group.apps, "workspace", intPtr(space.Index), "",
		))
	}
	return payload("yabai", "macOS Spaces", list, current)
}

func (backend *yabaiBackend) snapshot() (Snapshot, error) {
	spaceOutput, err := runCommand(backend.binary, []string{"-m", "query", "--spaces"}, childEnv(), 3*time.Second)
	if err != nil {
		return Snapshot{}, err
	}
	windowOutput, err := runCommand(backend.binary, []string{"-m", "query", "--windows"}, childEnv(), 3*time.Second)
	if err != nil {
		return Snapshot{}, err
	}
	var spaces []yabaiSpace
	var windows []yabaiWindow
	if err := json.Unmarshal([]byte(spaceOutput), &spaces); err != nil {
		return Snapshot{}, fmt.Errorf("could not parse yabai spaces: %w", err)
	}
	if err := json.Unmarshal([]byte(windowOutput), &windows); err != nil {
		return Snapshot{}, fmt.Errorf("could not parse yabai windows: %w", err)
	}
	return buildYabai(spaces, windows), nil
}

func (backend *yabaiBackend) activate(workspace Workspace) error {
	index, err := strconv.Atoi(workspace.ID)
	if err != nil {
		return fmt.Errorf("this macOS Space does not have a valid index")
	}
	_, err = runCommand(backend.binary, []string{"-m", "space", "--focus", strconv.Itoa(index)}, childEnv(), 3*time.Second)
	return err
}

type aerospaceBackend struct{ binary string }

func detectAeroSpace() workspaceBackend {
	if runtime.GOOS != "darwin" {
		return nil
	}
	binary := lookPath("aerospace")
	if binary == "" {
		return nil
	}
	if _, err := runCommand(binary, []string{"list-workspaces", "--all"}, childEnv(), 3*time.Second); err != nil {
		return nil
	}
	return &aerospaceBackend{binary: binary}
}

func parseAeroSpaceWorkspaces(output string) []struct {
	Name    string
	Focused bool
	Visible bool
	Monitor string
} {
	type row struct {
		Name    string
		Focused bool
		Visible bool
		Monitor string
	}
	rows := []row{}
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Split(line, "\t")
		if len(fields) < 5 || fields[0] == "" {
			continue
		}
		monitor := fields[4]
		if monitor == "" {
			monitor = "Monitor " + fields[3]
		}
		rows = append(rows, row{Name: fields[0], Focused: fields[1] == "true", Visible: fields[2] == "true", Monitor: monitor})
	}
	// The anonymous type above keeps AeroSpace parsing dependency-free.
	result := make([]struct {
		Name    string
		Focused bool
		Visible bool
		Monitor string
	}, len(rows))
	for index, item := range rows {
		result[index].Name = item.Name
		result[index].Focused = item.Focused
		result[index].Visible = item.Visible
		result[index].Monitor = item.Monitor
	}
	return result
}

func buildAeroSpace(workspacesText, windowsText string) Snapshot {
	groups := map[string]*appBucket{}
	for _, line := range strings.Split(windowsText, "\n") {
		fields := strings.Split(line, "\t")
		if len(fields) < 3 || fields[0] == "" {
			continue
		}
		group := bucketFor(groups, fields[0])
		group.windows++
		addApp(group, fields[1], fields[2])
	}
	rows := parseAeroSpaceWorkspaces(workspacesText)
	list := make([]Workspace, 0, len(rows))
	current := ""
	for _, row := range rows {
		group := groups[row.Name]
		if group == nil {
			group = newBucket()
		}
		if row.Focused {
			current = row.Name
		}
		list = append(list, makeWorkspace(
			row.Name, row.Name, row.Name, row.Monitor, row.Focused,
			group.windows, group.apps, "workspace", nil, "",
		))
	}
	return payload("aerospace", "AeroSpace", list, current)
}

func (backend *aerospaceBackend) snapshot() (Snapshot, error) {
	workspaces, err := runCommand(backend.binary, []string{
		"list-workspaces", "--all", "--format",
		"%{workspace}\t%{workspace-is-focused}\t%{workspace-is-visible}\t%{monitor-id}\t%{monitor-name}",
	}, childEnv(), 3*time.Second)
	if err != nil {
		return Snapshot{}, err
	}
	windows, err := runCommand(backend.binary, []string{
		"list-windows", "--all", "--format",
		"%{workspace}\t%{app-name}\t%{window-title}",
	}, childEnv(), 3*time.Second)
	if err != nil {
		return Snapshot{}, err
	}
	return buildAeroSpace(workspaces, windows), nil
}

func (backend *aerospaceBackend) activate(workspace Workspace) error {
	if workspace.Name == "" {
		return fmt.Errorf("this AeroSpace workspace has no name")
	}
	_, err := runCommand(backend.binary, []string{"workspace", workspace.Name}, childEnv(), 3*time.Second)
	return err
}

type winApp struct {
	Name  string `json:"name"`
	Title string `json:"title"`
}

type winDesktop struct {
	Index   int      `json:"index"`
	Name    string   `json:"name"`
	Current bool     `json:"current"`
	Apps    []winApp `json:"apps"`
}

type windowsBackend struct{ powershell string }

const powershellSnapshot = `
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
`

func powershellString(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func powershellImport() string {
	manifest, _ := filepath.Abs(filepath.Join("backend", "vendor", "psvirtualdesktop", "VirtualDesktop.psd1"))
	return "Import-Module " + powershellString(manifest) + " -Force -WarningAction SilentlyContinue; "
}

func detectWindows() workspaceBackend {
	if runtime.GOOS != "windows" {
		return nil
	}
	if sessionID() == 0 {
		return nil
	}
	command := lookPath("powershell.exe")
	if command == "" {
		command = lookPath("pwsh.exe")
	}
	if command == "" {
		return nil
	}
	manifest, _ := filepath.Abs(filepath.Join("backend", "vendor", "psvirtualdesktop", "VirtualDesktop.psd1"))
	if _, err := os.Stat(manifest); err != nil {
		return nil
	}
	script := "$ErrorActionPreference = 'SilentlyContinue'; " + powershellImport() + "if (Get-Command Get-DesktopCount -ErrorAction SilentlyContinue) { 'yes' }"
	output, err := runCommand(command, []string{"-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", script}, childEnv(), 10*time.Second)
	if err != nil || !strings.Contains(output, "yes") {
		return nil
	}
	return &windowsBackend{powershell: command}
}

func buildWindows(desktops []winDesktop) Snapshot {
	list := make([]Workspace, 0, len(desktops))
	current := ""
	for _, desktop := range desktops {
		apps := make([]App, 0, len(desktop.Apps))
		for _, app := range desktop.Apps {
			apps = append(apps, App{Name: safeText(app.Name, 48), Title: safeText(app.Title, 160)})
		}
		currentValue := desktop.Current
		if currentValue {
			current = strconv.Itoa(desktop.Index)
		}
		label := safeText(desktop.Name, 100)
		if label == "" {
			label = fmt.Sprintf("Desktop %d", desktop.Index+1)
		}
		list = append(list, makeWorkspace(
			strconv.Itoa(desktop.Index), label, label, "", desktop.Current,
			len(apps), apps, "workspace", intPtr(desktop.Index+1), "",
		))
	}
	return payload("windows", "Windows", list, current)
}

func (backend *windowsBackend) snapshot() (Snapshot, error) {
	script := powershellImport() + powershellSnapshot
	output, err := runCommand(backend.powershell, []string{
		"-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", script,
	}, childEnv(), 15*time.Second)
	if err != nil {
		return Snapshot{}, err
	}
	var desktops []winDesktop
	if err := json.Unmarshal([]byte(output), &desktops); err != nil {
		return Snapshot{}, fmt.Errorf("could not parse Windows desktops: %w", err)
	}
	return buildWindows(desktops), nil
}

func (backend *windowsBackend) activate(workspace Workspace) error {
	index, err := strconv.Atoi(workspace.ID)
	if err != nil {
		return fmt.Errorf("this Windows desktop does not have a valid index")
	}
	script := powershellImport() + fmt.Sprintf("Switch-Desktop -Desktop %d", index)
	_, err = runCommand(backend.powershell, []string{
		"-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", script,
	}, childEnv(), 15*time.Second)
	return err
}

func detectBackend() workspaceBackend {
	switch runtime.GOOS {
	case "linux":
		for _, detector := range []func() workspaceBackend{
			detectHyprland, detectSway, detectI3, detectWmctrl, detectKDE,
		} {
			if backend := detector(); backend != nil {
				return backend
			}
		}
	case "darwin":
		if backend := detectAeroSpace(); backend != nil {
			return backend
		}
		return detectYabai()
	case "windows":
		return detectWindows()
	}
	return nil
}

type manager struct{}

func (manager) snapshot() Snapshot {
	if runtime.GOOS == "windows" && sessionID() == 0 {
		return unsupported("Windows virtual desktops are only available in the logged-in desktop session.", unsupportedHint())
	}
	backend := detectBackend()
	if backend == nil {
		return unsupported("No supported workspace controller was found.", unsupportedHint())
	}
	snapshot, err := backend.snapshot()
	if err != nil {
		return unsupported(err.Error(), unsupportedHint())
	}
	return snapshot
}

func (manager) activate(id string) activation {
	if runtime.GOOS == "windows" && sessionID() == 0 {
		return activation{OK: false, Available: false, Error: "Windows virtual desktops are only available in the logged-in desktop session.", Hint: unsupportedHint()}
	}
	backend := detectBackend()
	if backend == nil {
		return activation{OK: false, Available: false, Error: "No supported workspace controller was found.", Hint: unsupportedHint()}
	}
	snapshot, err := backend.snapshot()
	if err != nil {
		return activation{OK: false, Available: false, Error: err.Error(), Hint: unsupportedHint()}
	}
	for _, workspace := range snapshot.Workspaces {
		if workspace.ID == safeText(id, 100) {
			if err := backend.activate(workspace); err != nil {
				return activation{OK: false, Available: true, Error: err.Error()}
			}
			return activation{OK: true, Available: true, Workspace: workspace}
		}
	}
	return activation{OK: false, Available: true, Error: "That workspace is no longer open."}
}

func max(left, right int) int {
	if left > right {
		return left
	}
	return right
}
