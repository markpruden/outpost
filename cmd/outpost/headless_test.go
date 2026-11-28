package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
)

func headlessMux(t *testing.T) (*Controller, *http.ServeMux) {
	c := chatController(t)
	c.tasks = map[string]*Task{}
	c.projects = map[string]*Project{}
	c.sessions = map[string]time.Time{}
	c.inferStub = func(ctx context.Context, node string, spec InferenceSpec) (*InferenceOutput, error) {
		<-ctx.Done() // the planner just waits; the test cancels it
		return nil, errors.New("stub: no model in this test")
	}
	t.Cleanup(func() { // stop the planners before the temp folder goes away
		c.mu.Lock()
		var list []*Task
		for _, tk := range c.tasks {
			list = append(list, tk)
		}
		c.mu.Unlock()
		for _, tk := range list {
			tk.mu.Lock()
			cancel := tk.cancel
			tk.mu.Unlock()
			if cancel != nil {
				cancel()
			}
		}
		time.Sleep(300 * time.Millisecond)
	})
	c.apiTokens = []apiToken{
		{ID: "r", Hash: hashAPIToken("run"), Scope: "run"},
		{ID: "c", Hash: hashAPIToken("chat"), Scope: "chat"},
		{ID: "f", Hash: hashAPIToken("full")},
	}
	m := http.NewServeMux()
	m.HandleFunc("POST /api/templates", c.auth(c.handleNewTemplate))
	m.HandleFunc("POST /api/templates/{id}", c.auth(c.handleEditTemplate))
	m.HandleFunc("POST /api/templates/{id}/run", c.authRun(c.handleRunTemplate))
	m.HandleFunc("GET /api/templates/{id}/status", c.authRun(c.handleTemplateStatus))
	m.HandleFunc("GET /api/tasks", c.auth(c.handleListTasks))
	m.HandleFunc("GET /v1/models", c.authAPI(c.handleV1Models))
	return c, m
}

func bearer(tok string) map[string]string { return map[string]string{"Authorization": "Bearer " + tok} }

// A "run" token starts headless saved tasks and reads their status, and nothing else.
func TestRunTokenScope(t *testing.T) {
	c, m := headlessMux(t)
	c.settings.DefaultPlanner = "node:n"
	rec := send(m, "POST", "/api/templates", `{"request":{"prompt":"nightly checks","planner":"node:n"},"headless":true}`, bearer("full"))
	var tpl Template
	json.Unmarshal(rec.Body.Bytes(), &tpl)
	if rec.Code != 200 || !tpl.Headless {
		t.Fatalf("new headless %d %s", rec.Code, rec.Body)
	}
	plain := send(m, "POST", "/api/templates", `{"request":{"prompt":"manual only","planner":"node:n"}}`, bearer("full"))
	var pl Template
	json.Unmarshal(plain.Body.Bytes(), &pl)

	// The run token reaches only the run and status routes.
	for _, p := range []string{"/api/tasks", "/v1/models"} {
		if rec := send(m, "GET", p, "", bearer("run")); rec.Code != 401 {
			t.Fatalf("run token on %s: %d", p, rec.Code)
		}
	}
	if rec := send(m, "POST", "/api/templates", `{"request":{"prompt":"x"}}`, bearer("run")); rec.Code != 401 {
		t.Fatalf("run token must not create saved tasks: %d", rec.Code)
	}
	if rec := send(m, "POST", "/api/templates/"+tpl.ID, `{"headless":false}`, bearer("run")); rec.Code != 401 {
		t.Fatalf("run token must not edit saved tasks: %d", rec.Code)
	}
	if rec := send(m, "POST", "/api/templates/"+tpl.ID+"/run", "", bearer("chat")); rec.Code != 401 {
		t.Fatalf("chat token on run: %d", rec.Code)
	}
	// A saved task that is not headless would wait for a person, so a run token cannot start it.
	if rec := send(m, "POST", "/api/templates/"+pl.ID+"/run", "", bearer("run")); rec.Code != 409 || !strings.Contains(rec.Body.String(), "not headless") {
		t.Fatalf("non-headless via run token: %d %s", rec.Code, rec.Body)
	}
	if rec := send(m, "GET", "/api/templates/"+tpl.ID+"/status", "", bearer("run")); rec.Code != 200 || !strings.Contains(rec.Body.String(), "never_run") {
		t.Fatalf("status before a run: %d %s", rec.Code, rec.Body)
	}
	// The headless run starts an unattended task and records it as a trigger.
	rec = send(m, "POST", "/api/templates/"+tpl.ID+"/run", "", bearer("run"))
	var made struct{ ID string }
	json.Unmarshal(rec.Body.Bytes(), &made)
	if rec.Code != 200 || made.ID == "" {
		t.Fatalf("run %d %s", rec.Code, rec.Body)
	}
	tk := c.getTask(made.ID)
	if tk == nil || !tk.Unattended || tk.Template != tpl.ID {
		t.Fatalf("task not unattended: %+v", tk)
	}
	if got := c.templates[0].Runs; len(got) != 1 || got[0].Kind != "trigger" {
		t.Fatalf("runs %+v", got)
	}
	// A second call while the first is still going is refused, like a schedule would skip it.
	tk.mu.Lock()
	tk.Status = "running"
	tk.mu.Unlock()
	if rec := send(m, "POST", "/api/templates/"+tpl.ID+"/run", "", bearer("run")); rec.Code != 409 || !strings.Contains(rec.Body.String(), "still going") {
		t.Fatalf("overlap: %d %s", rec.Code, rec.Body)
	}
	if rec := send(m, "GET", "/api/templates/"+tpl.ID+"/status", "", bearer("run")); rec.Code != 200 || !strings.Contains(rec.Body.String(), `"status":"running"`) || strings.Contains(rec.Body.String(), "prompt") {
		t.Fatalf("status: %d %s", rec.Code, rec.Body)
	}
	// A full-access token may still run a plain saved task by hand: not unattended.
	rec = send(m, "POST", "/api/templates/"+pl.ID+"/run", "", bearer("full"))
	json.Unmarshal(rec.Body.Bytes(), &made)
	if rec.Code != 200 || c.getTask(made.ID) == nil || c.getTask(made.ID).Unattended {
		t.Fatalf("manual run: %d %s", rec.Code, rec.Body)
	}
}

// Headless needs a planner on a node: nobody is there to cap a paid planner.
func TestHeadlessRefusesCloudPlanner(t *testing.T) {
	_, m := headlessMux(t)
	rec := send(m, "POST", "/api/templates", `{"request":{"prompt":"x","planner":"claude"},"headless":true}`, bearer("full"))
	if rec.Code != 400 || !strings.Contains(rec.Body.String(), "must plan on a node") {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
}

// Unattended tasks only use nodes that have a sandbox on; attended ones use any exec node.
func TestUnattendedNeedsSandbox(t *testing.T) {
	c := chatController(t)
	c.nodes = map[string]*NodeRec{
		"a": {ID: "a", ExecEnabled: true, Sandbox: "off"},
		"b": {ID: "b", ExecEnabled: true},
		"c": {ID: "c", ExecEnabled: true, Sandbox: "strict"},
	}
	c.conns = map[string]*nodeConn{"a": {}, "b": {}, "c": {}}
	c.lastHB = map[string]*Heartbeat{"a": {}, "b": {}, "c": {}}
	c.settings.Sandbox = "basic"
	attended, unattended := &Task{}, &Task{Unattended: true}
	if got := strings.Join(c.execNodesFor(attended), ","); got != "a,b,c" {
		t.Fatalf("attended: %s", got)
	}
	if got := strings.Join(c.execNodesFor(unattended), ","); got != "b,c" {
		t.Fatalf("unattended: %s", got)
	}
	c.settings.Sandbox = "off" // the global switch is off: only the node that chose a mode qualifies
	if got := strings.Join(c.execNodesFor(unattended), ","); got != "c" {
		t.Fatalf("global off: %s", got)
	}
}

// An unattended task that runs past the limit is stopped; an attended one is left alone.
func TestUnattendedTimeLimit(t *testing.T) {
	c := chatController(t)
	old := time.Now().Add(-unattendedMaxRun - time.Minute)
	dir := t.TempDir() // setStatus saves the task, so give each one a folder
	long := &Task{ID: "u", dir: dir, Unattended: true, Status: "running", Created: old}
	manual := &Task{ID: "m", dir: dir, Status: "running", Created: old}
	recent := &Task{ID: "r", dir: dir, Unattended: true, Status: "running", Created: time.Now()}
	c.tasks = map[string]*Task{"u": long, "m": manual, "r": recent}
	c.stopOverlongUnattended(time.Now())
	if long.Status != "failed" || manual.Status != "running" || recent.Status != "running" {
		t.Fatalf("u=%s m=%s r=%s", long.Status, manual.Status, recent.Status)
	}
}
