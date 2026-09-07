package controller

import (
	"testing"
	"time"

	"github.com/k-p2p-lab/v3/internal/model"
)

func TestStoppedPeerCannotBeRevivedByLaterHeartbeat(t *testing.T) {
	for _, lateState := range []string{model.NodeStarting, model.NodeReady, model.NodeStopping, model.NodeFailed} {
		t.Run(lateState, func(t *testing.T) {
			server := New(ServerConfig{DataDir: t.TempDir()}, nil)
			agent, err := server.state.registerAgent(model.Agent{ID: "agent", URL: "http://agent", Capacity: 2})
			if err != nil {
				t.Fatal(err)
			}
			stopped := model.Node{ID: "ended", State: model.NodeStopped, Metadata: map[string]string{"stoppedAt": "confirmed"}}
			if err := server.state.heartbeat(model.AgentHeartbeat{Agent: agent, Nodes: []model.Node{stopped}}); err != nil {
				t.Fatal(err)
			}
			agent.LastSeen = agent.LastSeen.Add(time.Second)
			late := model.Node{ID: "ended", State: lateState, Error: "outdated report", LastSeen: agent.LastSeen}
			if err := server.state.heartbeat(model.AgentHeartbeat{Agent: agent, Nodes: []model.Node{late}}); err != nil {
				t.Fatal(err)
			}
			if got := server.state.nodes["ended"]; got.State != model.NodeStopped || got.Error != "" || got.Metadata["stoppedAt"] != "confirmed" {
				t.Fatalf("late report revived completed exit: %+v", got)
			}
			if got := server.state.agents[agent.ID].ActiveNodes; got != 0 {
				t.Fatalf("late report restored logical occupancy: %d", got)
			}
		})
	}
}

func TestDelayedCreateResponseCannotUndoStopWithClockSkew(t *testing.T) {
	for _, terminal := range []string{model.NodeStopping, model.NodeStopped} {
		t.Run(terminal, func(t *testing.T) {
			server := New(ServerConfig{DataDir: t.TempDir()}, nil)
			now := time.Now().UTC()
			server.state.nodes["ended"] = model.Node{ID: "ended", AgentID: "agent", State: terminal, LastSeen: now}
			request := model.CreateNodeRequest{ID: "ended", RunID: "run", Group: "worker", Type: "full"}
			server.recordCreatedNode(request, "agent", model.Node{ID: "ended", State: model.NodeStarting, LastSeen: now.Add(time.Hour)})
			if got := server.state.nodes["ended"].State; got != terminal {
				t.Fatalf("delayed response changed %s to %s", terminal, got)
			}
		})
	}
}

func TestFailedCleanupCanStillComplete(t *testing.T) {
	server := New(ServerConfig{DataDir: t.TempDir()}, nil)
	agent, err := server.state.registerAgent(model.Agent{ID: "agent", URL: "http://agent", Capacity: 1})
	if err != nil {
		t.Fatal(err)
	}
	for _, state := range []string{model.NodeFailed, model.NodeStopping, model.NodeStopped} {
		agent.LastSeen = agent.LastSeen.Add(time.Second)
		node := model.Node{ID: "retry", State: state}
		if state == model.NodeFailed {
			node.Error = "container removal failed"
		}
		if err := server.state.heartbeat(model.AgentHeartbeat{Agent: agent, Nodes: []model.Node{node}}); err != nil {
			t.Fatal(err)
		}
		if got := server.state.nodes[node.ID]; got.State != state || got.Error != node.Error {
			t.Fatalf("cleanup transition to %s rejected: %+v", state, got)
		}
	}
}
