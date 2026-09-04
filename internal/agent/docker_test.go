package agent

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/k-p2p-lab/v3/internal/model"
)

const fakeContainerID = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

type dockerInvocation struct {
	Args  []string `json:"args"`
	Input []byte   `json:"input,omitempty"`
}

// Each CLI command runs in a helper subprocess, exercising real stdin, command
// cancellation and exit handling without requiring a Docker daemon in unit CI.
func TestDockerCLIHelper(t *testing.T) {
	if os.Getenv("KPL_DOCKER_HELPER") != "1" {
		return
	}
	var args []string
	for i, arg := range os.Args {
		if arg == "--" {
			args = os.Args[i+1:]
			break
		}
	}
	if len(args) == 0 {
		os.Exit(2)
	}
	input, err := io.ReadAll(os.Stdin)
	if err != nil {
		os.Exit(2)
	}
	file, err := os.OpenFile(os.Getenv("KPL_DOCKER_LOG"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		os.Exit(2)
	}
	_ = json.NewEncoder(file).Encode(dockerInvocation{Args: args, Input: input})
	_ = file.Close()
	if os.Getenv("KPL_DOCKER_HANG") == args[0] {
		for {
			time.Sleep(time.Hour)
		}
	}
	for _, fail := range strings.Split(os.Getenv("KPL_DOCKER_FAIL"), ",") {
		if fail == args[0] {
			fmt.Fprint(os.Stderr, "daemon unavailable during "+args[0])
			os.Exit(1)
		}
	}
	switch args[0] {
	case "info", "image":
		fmt.Print("linux\n")
	case "network":
		result := os.Getenv("KPL_DOCKER_NETWORK")
		if result == "" {
			result = `[{"Name":"kpl-v3-peers","Driver":"bridge","Attachable":false}]`
		}
		fmt.Print(result)
	case "create":
		fmt.Println(fakeContainerID)
	case "cp":
	case "start":
		fmt.Println(fakeContainerID)
	case "inspect":
		fmt.Print(`{"kpl-v3-peers":{"IPAddress":"172.25.0.8"}}`)
	case "wait":
		code := os.Getenv("KPL_DOCKER_EXIT")
		if code == "" {
			code = "0"
		}
		fmt.Println(code)
	case "logs":
		fmt.Fprint(os.Stderr, "tc: specified qdisc kind is unknown")
	case "ps":
		fmt.Print(os.Getenv("KPL_DOCKER_PS"))
	case "rm":
		if os.Getenv("KPL_DOCKER_MISSING") == "1" {
			fmt.Fprint(os.Stderr, "Error response from daemon: No such container: "+args[len(args)-1])
			os.Exit(1)
		}
		fmt.Println(args[len(args)-1])
	default:
		os.Exit(2)
	}
	os.Exit(0)
}

func fakeDocker(t *testing.T, settings map[string]string) (*dockerRuntime, string) {
	t.Helper()
	logPath := filepath.Join(t.TempDir(), "docker.jsonl")
	runtime := &dockerRuntime{binary: "fake-docker", image: "kpl-v3:local", network: "kpl-v3-peers"}
	runtime.commandContext = func(ctx context.Context, binary string, args ...string) *exec.Cmd {
		if binary != "fake-docker" {
			t.Errorf("binary = %q", binary)
		}
		command := exec.CommandContext(ctx, os.Args[0], append([]string{"-test.run=^TestDockerCLIHelper$", "--"}, args...)...)
		command.Env = append(os.Environ(), "KPL_DOCKER_HELPER=1", "KPL_DOCKER_LOG="+logPath)
		for name, value := range settings {
			command.Env = append(command.Env, "KPL_DOCKER_"+name+"="+value)
		}
		return command
	}
	return runtime, logPath
}

func dockerCalls(t *testing.T, path string) []dockerInvocation {
	t.Helper()
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var calls []dockerInvocation
	decoder := json.NewDecoder(bytes.NewReader(data))
	for {
		var call dockerInvocation
		if err := decoder.Decode(&call); err == io.EOF {
			break
		} else if err != nil {
			t.Fatal(err)
		}
		calls = append(calls, call)
	}
	return calls
}

func dockerTestConfig(t *testing.T, impaired bool) (model.Node, []byte) {
	t.Helper()
	node := model.Node{AgentID: "agent-a", RunID: "run-a", ID: "node-1", Generation: 3}
	config := model.PeerProcessConfig{Node: node, APListen: "0.0.0.0:18000", P2PListen: "/ip4/0.0.0.0/tcp/20000", Token: "config-secret"}
	if impaired {
		config.NodeConfig.Network.Delay = "50ms"
	}
	data, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	return node, data
}

func TestDockerCreateIsolatesPeerAndCopiesConfiguration(t *testing.T) {
	for _, impaired := range []bool{false, true} {
		t.Run(fmt.Sprint(impaired), func(t *testing.T) {
			d, logPath := fakeDocker(t, nil)
			node, config := dockerTestConfig(t, impaired)
			id, apiURL, err := d.create(context.Background(), node, config)
			if err != nil {
				t.Fatal(err)
			}
			if id != fakeContainerID || apiURL != "http://172.25.0.8:18000" {
				t.Fatalf("create = %q, %q", id, apiURL)
			}
			calls := dockerCalls(t, logPath)
			var steps []string
			for _, call := range calls {
				steps = append(steps, call.Args[0])
			}
			if !reflect.DeepEqual(steps, []string{"create", "cp", "start", "inspect"}) {
				t.Fatalf("steps = %v", steps)
			}
			args := calls[0].Args
			joined := strings.Join(args, " ")
			for _, expected := range []string{"--network kpl-v3-peers", "--cap-drop ALL", "--security-opt no-new-privileges", "--label io.kpl.agent=agent-a", "--label io.kpl.run=run-a", "--label io.kpl.generation=3", "kpl-v3:local peer --config /etc/kpl-peer.json"} {
				if !strings.Contains(joined, expected) {
					t.Errorf("missing %q in %s", expected, joined)
				}
			}
			for _, arg := range args {
				for _, forbidden := range []string{"--privileged", "--publish", "-p", "--volume", "-v", "--mount", "--pid", "--ipc"} {
					if arg == forbidden {
						t.Errorf("unexpected host isolation bypass %s", arg)
					}
				}
			}
			if strings.Contains(joined, "--cap-add NET_ADMIN") != impaired {
				t.Errorf("NET_ADMIN does not match impairment: %s", joined)
			}
			if strings.Contains(joined, "config-secret") {
				t.Error("configuration token leaked to command arguments")
			}
			reader := tar.NewReader(bytes.NewReader(calls[1].Input))
			header, err := reader.Next()
			if err != nil {
				t.Fatal(err)
			}
			data, err := io.ReadAll(reader)
			if err != nil {
				t.Fatal(err)
			}
			if header.Name != "etc/kpl-peer.json" || header.Mode != 0600 || !bytes.Equal(data, config) {
				t.Fatalf("incorrect configuration archive: %+v", header)
			}
			if _, err := reader.Next(); err != io.EOF {
				t.Fatalf("unexpected extra archive entry: %v", err)
			}
		})
	}
}

func TestDockerCreateCleansUpFailures(t *testing.T) {
	for _, stage := range []string{"create", "cp", "start", "inspect"} {
		t.Run(stage, func(t *testing.T) {
			d, logPath := fakeDocker(t, map[string]string{"FAIL": stage})
			node, config := dockerTestConfig(t, false)
			id, apiURL, err := d.create(context.Background(), node, config)
			if err == nil || id != "" || apiURL != "" {
				t.Fatalf("failed create = %q, %q, %v", id, apiURL, err)
			}
			calls := dockerCalls(t, logPath)
			last := calls[len(calls)-1].Args
			if len(last) != 3 || last[0] != "rm" || last[1] != "--force" {
				t.Fatalf("cleanup = %v", last)
			}
			if stage == "create" {
				if !dockerPeerName.MatchString(last[2]) {
					t.Fatalf("cleanup name = %q", last[2])
				}
			} else if last[2] != fakeContainerID {
				t.Fatalf("cleanup ID = %q", last[2])
			}
		})
	}
}

func TestDockerCreateCancellationCleansUpWithIndependentContext(t *testing.T) {
	for _, stage := range []string{"create", "start"} {
		t.Run(stage, func(t *testing.T) {
			d, logPath := fakeDocker(t, map[string]string{"HANG": stage})
			node, config := dockerTestConfig(t, false)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			go func() { _, _, err := d.create(ctx, node, config); done <- err }()
			deadline := time.Now().Add(10 * time.Second)
			found := false
			for time.Now().Before(deadline) {
				for _, call := range dockerCalls(t, logPath) {
					if call.Args[0] == stage {
						found = true
					}
				}
				if found {
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
			cancel()
			select {
			case err := <-done:
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("create cancellation error = %v", err)
				}
			case <-time.After(10 * time.Second):
				t.Fatal("create failed to finish after cancellation")
			}
			if !found {
				t.Fatal("helper did not reach intended cancellation point")
			}
			calls := dockerCalls(t, logPath)
			if calls[len(calls)-1].Args[0] != "rm" {
				t.Fatal("cleanup did not run after cancellation")
			}
		})
	}
}

func TestDockerCleanupReportsDaemonFailuresAndAcceptsAbsence(t *testing.T) {
	d, _ := fakeDocker(t, map[string]string{"FAIL": "cp,rm"})
	node, config := dockerTestConfig(t, false)
	id, apiURL, err := d.create(context.Background(), node, config)
	if err == nil || !strings.Contains(err.Error(), "copy peer container configuration") || !errors.Is(err, errDockerCleanup) {
		t.Fatalf("error = %v", err)
	}
	if id != fakeContainerID || apiURL != "" {
		t.Fatalf("cleanup failure must retain retry target, got ID %q and URL %q", id, apiURL)
	}
	d, _ = fakeDocker(t, map[string]string{"FAIL": "create,rm"})
	id, _, err = d.create(context.Background(), node, config)
	if !errors.Is(err, errDockerCleanup) || !dockerPeerName.MatchString(id) {
		t.Fatalf("failed create must retain attempt name for cleanup retry, got ID %q and error %v", id, err)
	}
	d, _ = fakeDocker(t, map[string]string{"MISSING": "1"})
	if err := d.remove(context.Background(), fakeContainerID); err != nil {
		t.Fatalf("remove missing container: %v", err)
	}
	if err := d.remove(context.Background(), "unrelated-container"); err == nil {
		t.Fatal("arbitrary container name accepted")
	}
}

func TestDockerWaitReportsPeerFailureAndLogs(t *testing.T) {
	for _, code := range []string{"0", "7", "invalid"} {
		t.Run(code, func(t *testing.T) {
			d, _ := fakeDocker(t, map[string]string{"EXIT": code})
			err := d.wait(context.Background(), fakeContainerID)
			if code == "0" && err != nil {
				t.Fatal(err)
			}
			if code == "7" && (err == nil || !strings.Contains(err.Error(), "code 7") || !strings.Contains(err.Error(), "qdisc kind is unknown")) {
				t.Fatalf("error = %v", err)
			}
			if code == "invalid" && (err == nil || !strings.Contains(err.Error(), "invalid exit code")) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestDockerCheckRejectsSharedNetworkAndNonAttachableOverlay(t *testing.T) {
	for _, test := range []struct {
		name, network, response string
		valid                   bool
	}{
		{"bridge", "kpl-v3-peers", `[{"Name":"kpl-v3-peers","Driver":"bridge"}]`, true},
		{"overlay", "kpl-v3-peers", `[{"Name":"kpl-v3-peers","Driver":"overlay","Attachable":true}]`, true},
		{"host", "host", "", false},
		{"builtin", "bridge", "", false},
		{"builtin-id", "network-id", `[{"Name":"bridge","Driver":"bridge"}]`, false},
		{"non-attachable", "kpl-v3-peers", `[{"Name":"kpl-v3-peers","Driver":"overlay","Attachable":false}]`, false},
		{"host-driver", "kpl-v3-peers", `[{"Name":"kpl-v3-peers","Driver":"host"}]`, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			d, _ := fakeDocker(t, map[string]string{"NETWORK": test.response})
			d.network = test.network
			err := d.check(context.Background())
			if (err == nil) != test.valid {
				t.Fatalf("check error = %v, valid = %v", err, test.valid)
			}
		})
	}
}

func TestDockerReconcileOnlyListsManagedAgentContainers(t *testing.T) {
	d, logPath := fakeDocker(t, map[string]string{"PS": fakeContainerID + "\n"})
	if err := d.removeAgentContainers(context.Background(), "agent-a"); err != nil {
		t.Fatal(err)
	}
	calls := dockerCalls(t, logPath)
	if len(calls) != 2 {
		t.Fatalf("calls = %v", calls)
	}
	if want := []string{"ps", "--all", "--quiet", "--no-trunc", "--filter", "label=io.kpl.managed=true", "--filter", "label=io.kpl.agent=agent-a", "--filter", "label=io.kpl.network=kpl-v3-peers"}; !reflect.DeepEqual(calls[0].Args, want) {
		t.Fatalf("list args = %v", calls[0].Args)
	}
	if calls[1].Args[2] != fakeContainerID {
		t.Fatalf("remove args = %v", calls[1].Args)
	}
}
