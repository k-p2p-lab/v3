package agent

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/k-p2p-lab/v3/internal/model"
)

const peerHelperEnvironment = "GO_WANT_KPL_AGENT_PEER_HELPER"

func TestAgentPeerHelperProcess(t *testing.T) {
	if os.Getenv(peerHelperEnvironment) != "1" {
		return
	}
	for {
		time.Sleep(time.Hour)
	}
}

func TestCreateNodeReusesPortsAfterProcessExit(t *testing.T) {
	server := newRuntimeTestServer(t, 65534, 65535)
	server.commandContext = peerHelperCommand
	t.Cleanup(func() { stopRuntimeTestProcesses(t, server) })

	request := model.CreateNodeRequest{
		ID: "first", RunID: "run", Group: "workers", Type: "full",
		Config: model.NodeConfig{GossipSub: model.GossipSubConfig{Topics: []string{"topic,with,comma"}}},
	}
	first, err := server.createNode(context.Background(), request)
	if err != nil {
		t.Fatalf("create first node: %v", err)
	}
	var topics []string
	if err := json.Unmarshal([]byte(first.Metadata["topicsJSON"]), &topics); err != nil {
		t.Fatalf("decode lossless topic metadata: %v", err)
	}
	if want := []string{"topic,with,comma"}; !reflect.DeepEqual(topics, want) {
		t.Fatalf("topicsJSON = %v, want %v", topics, want)
	}

	firstAPI, firstP2P := processPorts(t, server, request.ID)
	if err := server.stopNode(request.ID); err != nil {
		t.Fatalf("stop first node: %v", err)
	}
	waitForNodeState(t, server, request.ID, model.NodeStopped)

	request.ID = "second"
	if _, err := server.createNode(context.Background(), request); err != nil {
		t.Fatalf("create second node with released one-slot range: %v", err)
	}
	secondAPI, secondP2P := processPorts(t, server, request.ID)
	if secondAPI != firstAPI || secondP2P != firstP2P {
		t.Fatalf("second node ports = %d/%d, want reused %d/%d", secondAPI, secondP2P, firstAPI, firstP2P)
	}
	if err := server.stopNode(request.ID); err != nil {
		t.Fatalf("stop second node: %v", err)
	}
	waitForNodeState(t, server, request.ID, model.NodeStopped)
}

func TestCreateNodeReturnsPortsAfterStartFailure(t *testing.T) {
	server := newRuntimeTestServer(t, 65534, 65535)
	server.config.Executable = filepath.Join(t.TempDir(), "missing-peer-executable")
	request := model.CreateNodeRequest{ID: "first", RunID: "run", Group: "workers", Type: "full"}

	if _, err := server.createNode(context.Background(), request); err == nil || !strings.Contains(err.Error(), "start peer process") {
		t.Fatalf("first create error = %v, want start failure", err)
	}
	request.ID = "second"
	if _, err := server.createNode(context.Background(), request); err == nil || !strings.Contains(err.Error(), "start peer process") {
		t.Fatalf("second create error = %v, want another start failure instead of exhausted ports", err)
	}
}

func TestRunGenerationFenceRejectsLateCreate(t *testing.T) {
	server := newRuntimeTestServer(t, 65534, 65535)
	server.commandContext = peerHelperCommand
	t.Cleanup(func() { stopRuntimeTestProcesses(t, server) })
	server.stopRunGeneration("run", 2)

	for index, generation := range []uint64{0, 2} {
		_, err := server.createNode(context.Background(), model.CreateNodeRequest{
			ID: "late-" + string(rune('a'+index)), RunID: "run", Generation: generation, Group: "workers", Type: "full",
		})
		if err == nil || !strings.Contains(err.Error(), "is fenced") {
			t.Fatalf("generation %d create error = %v, want fence rejection", generation, err)
		}
	}

	node, err := server.createNode(context.Background(), model.CreateNodeRequest{
		ID: "current", RunID: "run", Generation: 3, Group: "workers", Type: "full",
	})
	if err != nil {
		t.Fatalf("create newer generation: %v", err)
	}
	if node.Generation != 3 {
		t.Fatalf("created node generation = %d, want 3", node.Generation)
	}
	server.stopRunGeneration("run", 3)
	waitForNodeState(t, server, node.ID, model.NodeStopped)
}

func TestStopRunGenerationOnlyStopsFencedProcesses(t *testing.T) {
	canceledOld := make(chan struct{}, 2)
	canceledNew := make(chan struct{}, 1)
	canceledOther := make(chan struct{}, 1)
	server := &Server{processes: map[string]*process{
		"legacy": {
			node:   model.Node{ID: "legacy", RunID: "run", Generation: 0, State: model.NodeReady},
			cancel: func() { canceledOld <- struct{}{} },
		},
		"old": {
			node:   model.Node{ID: "old", RunID: "run", Generation: 2, State: model.NodeReady},
			cancel: func() { canceledOld <- struct{}{} },
		},
		"new": {
			node:   model.Node{ID: "new", RunID: "run", Generation: 3, State: model.NodeReady},
			cancel: func() { canceledNew <- struct{}{} },
		},
		"other": {
			node:   model.Node{ID: "other", RunID: "other-run", Generation: 1, State: model.NodeReady},
			cancel: func() { canceledOther <- struct{}{} },
		},
	}}

	server.stopRunGeneration("run", 2)
	if len(canceledOld) != 2 {
		t.Fatalf("canceled old process count = %d, want 2", len(canceledOld))
	}
	if len(canceledNew) != 0 || len(canceledOther) != 0 {
		t.Fatal("newer or unrelated process was canceled")
	}
	server.mu.RLock()
	fence, fenced := server.runFences["run"]
	legacyState := server.processes["legacy"].node.State
	oldState := server.processes["old"].node.State
	newState := server.processes["new"].node.State
	otherState := server.processes["other"].node.State
	server.mu.RUnlock()
	if !fenced || fence != 2 {
		t.Fatalf("run fence = %d/%t, want 2/true", fence, fenced)
	}
	if legacyState != model.NodeStopping || oldState != model.NodeStopping {
		t.Fatalf("old states = %q/%q, want stopping", legacyState, oldState)
	}
	if newState != model.NodeReady || otherState != model.NodeReady {
		t.Fatalf("unfenced states = %q/%q, want ready", newState, otherState)
	}

	server.stopRunGeneration("run", 1)
	server.mu.RLock()
	fence = server.runFences["run"]
	server.mu.RUnlock()
	if fence != 2 {
		t.Fatalf("older stop lowered run fence to %d", fence)
	}
}

func TestRunGenerationStopAPI(t *testing.T) {
	canceled := make(chan struct{}, 1)
	server := &Server{processes: map[string]*process{
		"node": {
			node:   model.Node{ID: "node", RunID: "run-a", Generation: 4, State: model.NodeReady},
			cancel: func() { canceled <- struct{}{} },
		},
	}}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodDelete, "/api/v1/runs/run-a/nodes?generation=4", nil)
	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("stop run response = %d, want %d: %s", recorder.Code, http.StatusAccepted, recorder.Body.String())
	}
	if len(canceled) != 1 {
		t.Fatal("run stop endpoint did not cancel matching process")
	}

	recorder = httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodDelete, "/api/v1/runs/run-a/nodes?generation=invalid", nil)
	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("invalid generation response = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
}

func TestInFlightPublishPinsPortsUntilRequestCompletes(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(started)
		<-release
		w.WriteHeader(http.StatusAccepted)
	}))
	defer peer.Close()
	defer unblock()

	server := &Server{client: peer.Client(), processes: make(map[string]*process)}
	lease, ok := server.ports.acquire(65534, 65535)
	if !ok {
		t.Fatal("allocate process ports")
	}
	proc := &process{node: model.Node{ID: "node", State: model.NodeReady}, apiURL: peer.URL, lease: lease}
	server.processes[proc.node.ID] = proc

	published := make(chan error, 1)
	go func() {
		published <- server.proxyPublish(context.Background(), proc.node.ID, model.PublishRequest{PayloadSize: 1})
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("publish did not reach peer")
	}

	server.mu.Lock()
	proc.exited = true
	proc.node.State = model.NodeStopped
	server.releaseProcessPortsLocked(proc)
	_, allocatedWhilePinned := server.ports.acquire(65534, 65535)
	server.mu.Unlock()
	if allocatedWhilePinned {
		t.Fatal("ports were reused while an old proxy request still referenced them")
	}

	if err := server.updateNode(model.Node{ID: proc.node.ID, State: model.NodeReady}); err != nil {
		t.Fatalf("apply late status: %v", err)
	}
	server.mu.RLock()
	state := proc.node.State
	server.mu.RUnlock()
	if state != model.NodeStopped {
		t.Fatalf("late status resurrected exited process as %s", state)
	}

	unblock()
	select {
	case err := <-published:
		if err != nil {
			t.Fatalf("publish failed: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("publish did not finish")
	}

	server.mu.Lock()
	reused, ok := server.ports.acquire(65534, 65535)
	server.mu.Unlock()
	if !ok {
		t.Fatal("ports were not released after process exit and proxy completion")
	}
	server.mu.Lock()
	server.ports.release(reused)
	server.mu.Unlock()
}

func TestProxyPublishConcurrentStateTransitions(t *testing.T) {
	server := &Server{
		client: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusAccepted, Status: "202 Accepted", Body: http.NoBody}, nil
		})},
		processes: map[string]*process{
			"node": {node: model.Node{ID: "node", State: model.NodeReady}, apiURL: "http://peer.invalid"},
		},
	}
	start := make(chan struct{})
	var group sync.WaitGroup
	for worker := 0; worker < 4; worker++ {
		group.Add(2)
		go func() {
			defer group.Done()
			<-start
			for i := 0; i < 1000; i++ {
				state := model.NodeReady
				if i%2 == 0 {
					state = model.NodeStarting
				}
				if err := server.updateNode(model.Node{ID: "node", State: state}); err != nil {
					t.Errorf("update node: %v", err)
					return
				}
			}
		}()
		go func() {
			defer group.Done()
			<-start
			for i := 0; i < 1000; i++ {
				_ = server.proxyPublish(context.Background(), "node", model.PublishRequest{PayloadSize: 1})
			}
		}()
	}
	close(start)
	group.Wait()
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func newRuntimeTestServer(t *testing.T, apiPort, p2pPort int) *Server {
	t.Helper()
	server, err := New(Config{
		ID:            "agent-test",
		ControllerURL: "http://127.0.0.1:1",
		AdvertiseURL:  "http://127.0.0.1:8090",
		Capacity:      1,
		DataDir:       t.TempDir(),
		Executable:    os.Args[0],
		PeerAPIPort:   apiPort,
		PeerP2PPort:   p2pPort,
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("create test agent: %v", err)
	}
	return server
}

func peerHelperCommand(ctx context.Context, _ string, _ ...string) *exec.Cmd {
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestAgentPeerHelperProcess$")
	command.Env = append(os.Environ(), peerHelperEnvironment+"=1")
	return command
}

func processPorts(t *testing.T, server *Server, nodeID string) (int, int) {
	t.Helper()
	server.mu.RLock()
	defer server.mu.RUnlock()
	proc, ok := server.processes[nodeID]
	if !ok || proc.lease == nil {
		t.Fatalf("process %q has no port lease", nodeID)
	}
	return proc.lease.apiPort, proc.lease.p2pPort
}

func waitForNodeState(t *testing.T, server *Server, nodeID, want string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		server.mu.RLock()
		proc := server.processes[nodeID]
		state := ""
		if proc != nil {
			state = proc.node.State
		}
		server.mu.RUnlock()
		if state == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("node %q did not reach state %q", nodeID, want)
}

func stopRuntimeTestProcesses(t *testing.T, server *Server) {
	t.Helper()
	server.stopAll()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		server.mu.RLock()
		allExited := true
		for _, proc := range server.processes {
			if !proc.exited {
				allExited = false
				break
			}
		}
		server.mu.RUnlock()
		if allExited {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Error("test peer processes did not exit")
}
