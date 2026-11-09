package main

import (
	"encoding/json"
	"flag"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

type serverState struct {
	token   string
	manager manager
}

func (state *serverState) authorized(response http.ResponseWriter, request *http.Request) bool {
	if state.token != "" && request.Header.Get("X-Orqella-Module-Token") == state.token {
		return true
	}
	writeJSON(response, http.StatusUnauthorized, map[string]any{"ok": false, "error": "unauthorized"})
	return false
}

func writeJSON(response http.ResponseWriter, status int, body any) {
	data, err := json.Marshal(body)
	if err != nil {
		http.Error(response, "could not encode response", http.StatusInternalServerError)
		return
	}
	response.Header().Set("Content-Type", "application/json")
	response.Header().Set("Cache-Control", "no-store")
	response.Header().Set("X-Content-Type-Options", "nosniff")
	response.WriteHeader(status)
	_, _ = response.Write(data)
}

func (state *serverState) snapshot(response http.ResponseWriter, request *http.Request) {
	if !state.authorized(response, request) {
		return
	}
	writeJSON(response, http.StatusOK, state.manager.snapshot())
}

func (state *serverState) activate(response http.ResponseWriter, request *http.Request) {
	if !state.authorized(response, request) {
		return
	}
	if request.Method != http.MethodPost {
		writeJSON(response, http.StatusMethodNotAllowed, map[string]any{"ok": false, "error": "method not allowed"})
		return
	}
	body, err := io.ReadAll(io.LimitReader(request.Body, 65536))
	if err != nil {
		writeJSON(response, http.StatusBadRequest, map[string]any{"ok": false, "error": "could not read request"})
		return
	}
	var payload struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body, &payload); err != nil || strings.TrimSpace(payload.ID) == "" {
		writeJSON(response, http.StatusBadRequest, map[string]any{"ok": false, "error": "workspace id is required"})
		return
	}
	writeJSON(response, http.StatusOK, state.manager.activate(payload.ID))
}

func (state *serverState) health(response http.ResponseWriter, request *http.Request) {
	if !state.authorized(response, request) {
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"ok": true})
}

func main() {
	snapshotMode := flag.Bool("snapshot", false, "print one workspace snapshot as JSON")
	flag.Parse()
	if *snapshotMode {
		data, err := json.MarshalIndent(manager{}.snapshot(), "", "  ")
		if err != nil {
			log.Fatal(err)
		}
		_, _ = os.Stdout.Write(append(data, '\n'))
		return
	}

	token := os.Getenv("ORQELLA_MODULE_TOKEN")
	port, err := strconv.Atoi(os.Getenv("ORQELLA_MODULE_PORT"))
	if err != nil || port <= 0 || token == "" {
		log.Fatal("ORQELLA_MODULE_TOKEN and ORQELLA_MODULE_PORT are required")
	}

	state := &serverState{token: token}
	mux := http.NewServeMux()
	mux.HandleFunc("/health", state.health)
	mux.HandleFunc("/api/workspaces", state.snapshot)
	mux.HandleFunc("/api/workspaces/activate", state.activate)

	server := &http.Server{
		Addr:              "127.0.0.1:" + strconv.Itoa(port),
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	if err := server.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}
