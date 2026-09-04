package controller

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/k-p2p-lab/v3/internal/model"
)

func TestDashboardRESTSnapshotExposesAgentsNetworkAndJobs(t *testing.T) {
	server := New(ServerConfig{DataDir: t.TempDir()}, nil)
	now := time.Now().UTC()
	server.state.mu.Lock()
	server.state.agents["agent-a"] = model.Agent{
		ID: "agent-a", URL: "http://agent-a", Capacity: 10, ActiveNodes: 1,
		State: model.AgentOnline, LastSeen: now,
	}
	server.state.nodes["node-a"] = model.Node{
		ID: "node-a", RunID: "run-a", AgentID: "agent-a", Group: "workers",
		Role: "worker", Type: "full", PeerID: "peer-a", State: model.NodeReady,
		PeerScores: map[string]float64{"peer-b": 1.5}, LastSeen: now,
	}
	server.state.experiments["run-a"] = model.Experiment{
		ID: "run-a", Name: "rest-view", State: "running", ActiveJobs: 1,
		CompletedJobs: 2, FailedJobs: 3, CanceledJobs: 4, StartedAt: now,
	}
	server.state.mu.Unlock()

	handler := server.Handler(context.Background())
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/snapshot", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("snapshot status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var snapshot model.Snapshot
	if err := json.Unmarshal(recorder.Body.Bytes(), &snapshot); err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Agents) != 1 || len(snapshot.Nodes) != 1 || len(snapshot.Experiments) != 1 {
		t.Fatalf("incomplete snapshot: %+v", snapshot)
	}
	if snapshot.Nodes[0].PeerScores["peer-b"] != 1.5 {
		t.Fatalf("peer scores missing from REST snapshot: %+v", snapshot.Nodes[0].PeerScores)
	}
	run := snapshot.Experiments[0]
	if run.ActiveJobs != 1 || run.CompletedJobs != 2 || run.FailedJobs != 3 || run.CanceledJobs != 4 {
		t.Fatalf("job counters missing from REST snapshot: %+v", run)
	}

	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "K-P2PLab") {
		t.Fatalf("dashboard response status=%d body=%q", recorder.Code, recorder.Body.String())
	}
}

func TestBootstrapRegistryRequiresRunAndReturnsOnlyReadyBootNodesFromThatRun(t *testing.T) {
	server := New(ServerConfig{DataDir: t.TempDir()}, nil)
	server.state.mu.Lock()
	server.state.nodes["run-a-boot"] = model.Node{
		ID: "run-a-boot", RunID: "run-a", Role: "boot", State: model.NodeReady,
		PeerID: "peer-a", Addresses: []string{"/ip4/127.0.0.1/tcp/10001"},
	}
	server.state.nodes["run-b-boot"] = model.Node{
		ID: "run-b-boot", RunID: "run-b", Role: "boot", State: model.NodeReady,
		PeerID: "peer-b", Addresses: []string{"/ip4/127.0.0.1/tcp/10002"},
	}
	server.state.nodes["run-a-starting-boot"] = model.Node{
		ID: "run-a-starting-boot", RunID: "run-a", Role: "boot", State: model.NodeStarting,
		PeerID: "peer-starting", Addresses: []string{"/ip4/127.0.0.1/tcp/10003"},
	}
	server.state.nodes["run-a-worker"] = model.Node{
		ID: "run-a-worker", RunID: "run-a", Role: "worker", State: model.NodeReady,
		PeerID: "peer-worker", Addresses: []string{"/ip4/127.0.0.1/tcp/10004"},
	}
	server.state.mu.Unlock()

	handler := server.Handler(context.Background())
	for _, target := range []string{"/api/v1/bootstrap", "/api/v1/bootstrap?runId=%20%20"} {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, target, nil))
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("GET %s status=%d body=%s, want 400", target, recorder.Code, recorder.Body.String())
		}
	}

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/bootstrap?runId=run-a", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("bootstrap status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var nodes []struct {
		NodeID string `json:"nodeId"`
		PeerID string `json:"peerId"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &nodes); err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 1 || nodes[0].NodeID != "run-a-boot" || nodes[0].PeerID != "peer-a" {
		t.Fatalf("bootstrap nodes = %+v, want only ready run-a boot node", nodes)
	}
}
