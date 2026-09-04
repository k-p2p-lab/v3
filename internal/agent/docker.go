package agent

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/k-p2p-lab/v3/internal/model"
)

const (
	peerContainerAPIPort  = 18000
	peerContainerP2PPort  = 20000
	peerContainerConfig   = "/etc/kpl-peer.json"
	managedContainerLabel = "io.kpl.managed=true"
)

var (
	errDockerCleanup  = errors.New("Docker cleanup failed")
	dockerNetworkName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]*$`)
	dockerContainerID = regexp.MustCompile(`^[a-f0-9]{64}$`)
	dockerPeerName    = regexp.MustCompile(`^kpl-peer-[a-f0-9]{20}-[a-f0-9]{12}$`)
)

// dockerRuntime runs one peer in each private container network namespace. It
// never publishes host ports, shares the host network, or mounts the Docker
// socket (or another host path) into a peer container.
type dockerRuntime struct {
	binary         string
	image          string
	network        string
	commandContext func(context.Context, string, ...string) *exec.Cmd
}

func (d *dockerRuntime) check(ctx context.Context) error {
	if !dockerNetworkName.MatchString(d.network) || d.network == "host" || d.network == "none" || d.network == "bridge" {
		return fmt.Errorf("docker peer network must be a user-defined bridge or attachable overlay network")
	}
	if d.image == "" || strings.HasPrefix(d.image, "-") {
		return fmt.Errorf("docker peer image is required")
	}
	output, err := d.run(ctx, nil, "info", "--format", "{{.OSType}}")
	if err != nil {
		return fmt.Errorf("connect to Docker daemon: %w", err)
	}
	if strings.TrimSpace(string(output)) != "linux" {
		return fmt.Errorf("Docker peer runtime requires a Linux Docker daemon")
	}
	output, err = d.run(ctx, nil, "image", "inspect", "--format", "{{.Os}}", d.image)
	if err != nil {
		return fmt.Errorf("inspect peer image %q (build or pull it first): %w", d.image, err)
	}
	if strings.TrimSpace(string(output)) != "linux" {
		return fmt.Errorf("Docker peer image %q must be a Linux image", d.image)
	}
	output, err = d.run(ctx, nil, "network", "inspect", d.network)
	if err != nil {
		return fmt.Errorf("inspect peer network %q (create it first): %w", d.network, err)
	}
	var networks []struct {
		Name       string
		Driver     string
		Attachable bool
	}
	if err := json.Unmarshal(output, &networks); err != nil || len(networks) != 1 {
		return fmt.Errorf("invalid Docker network inspect response for %q", d.network)
	}
	network := networks[0]
	if network.Name == "bridge" || (network.Driver != "bridge" && network.Driver != "overlay") {
		return fmt.Errorf("peer network %q must use a user-defined bridge or overlay driver", d.network)
	}
	if network.Driver == "overlay" && !network.Attachable {
		return fmt.Errorf("peer overlay network %q must be attachable", d.network)
	}
	return nil
}

func (d *dockerRuntime) create(ctx context.Context, node model.Node, configData []byte) (id, apiURL string, err error) {
	var config model.PeerProcessConfig
	if err := json.Unmarshal(configData, &config); err != nil {
		return "", "", fmt.Errorf("decode peer container configuration: %w", err)
	}
	name, err := peerContainerName(node)
	if err != nil {
		return "", "", err
	}
	args := []string{"create", "--name", name,
		"--label", managedContainerLabel,
		"--label", "io.kpl.agent=" + node.AgentID,
		"--label", "io.kpl.run=" + node.RunID,
		"--label", "io.kpl.node=" + node.ID,
		"--label", "io.kpl.generation=" + strconv.FormatUint(node.Generation, 10),
		"--label", "io.kpl.network=" + d.network,
		"--network", d.network, "--user", "0:0", "--cap-drop", "ALL",
		"--security-opt", "no-new-privileges", "--init",
		"--log-driver", "json-file", "--log-opt", "max-size=10m", "--log-opt", "max-file=2",
	}
	if config.NodeConfig.Network.Enabled() {
		args = append(args, "--cap-add", "NET_ADMIN")
	}
	args = append(args, d.image, "peer", "--config", peerContainerConfig)
	// A unique attempt suffix lets cleanup find a container even if the CLI is
	// interrupted after daemon creation but before it returns the container ID.
	cleanupTarget := name
	defer func() {
		if err == nil {
			return
		}
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if cleanupErr := d.remove(cleanupCtx, cleanupTarget); cleanupErr != nil {
			err = errors.Join(err, fmt.Errorf("%w for peer container %s: %w", errDockerCleanup, cleanupTarget, cleanupErr))
			// Preserve the exact attempt ID/name so stop-all can retry a
			// transient cleanup failure without touching unrelated peers.
			id, apiURL = cleanupTarget, ""
			return
		}
		id, apiURL = "", ""
	}()
	output, err := d.run(ctx, nil, args...)
	if err != nil {
		return "", "", fmt.Errorf("create peer container: %w", err)
	}
	id = strings.TrimSpace(string(output))
	if !dockerContainerID.MatchString(id) {
		return "", "", fmt.Errorf("Docker create returned an invalid container ID")
	}
	cleanupTarget = id
	archive, err := peerConfigArchive(configData)
	if err != nil {
		return "", "", err
	}
	// docker cp accepts a tar stream and works before container startup, which
	// also supports a remote daemon without sharing the Agent's filesystem.
	if _, err := d.run(ctx, bytes.NewReader(archive), "cp", "-", id+":/"); err != nil {
		return "", "", fmt.Errorf("copy peer container configuration: %w", err)
	}
	if _, err := d.run(ctx, nil, "start", id); err != nil {
		return "", "", fmt.Errorf("start peer container: %w", err)
	}
	output, err = d.run(ctx, nil, "inspect", "--format", "{{json .NetworkSettings.Networks}}", id)
	if err != nil {
		return "", "", fmt.Errorf("inspect peer container address: %w", err)
	}
	var networks map[string]struct{ IPAddress string }
	if err := json.Unmarshal(output, &networks); err != nil {
		return "", "", fmt.Errorf("decode peer container address: %w", err)
	}
	ip := net.ParseIP(networks[d.network].IPAddress)
	if ip == nil || ip.To4() == nil || ip.IsLoopback() || ip.IsUnspecified() {
		return "", "", fmt.Errorf("peer container has no usable IPv4 address on network %q", d.network)
	}
	if err := ctx.Err(); err != nil {
		return "", "", err
	}
	return id, "http://" + net.JoinHostPort(ip.String(), strconv.Itoa(peerContainerAPIPort)), nil
}

func (d *dockerRuntime) wait(ctx context.Context, id string) error {
	if !dockerContainerID.MatchString(id) {
		return fmt.Errorf("invalid peer container ID")
	}
	output, err := d.run(ctx, nil, "wait", id)
	if err != nil {
		return fmt.Errorf("wait for peer container: %w", err)
	}
	code, err := strconv.Atoi(strings.TrimSpace(string(output)))
	if err != nil {
		return fmt.Errorf("Docker wait returned an invalid exit code")
	}
	if code != 0 {
		logCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		logs, logErr := d.run(logCtx, nil, "logs", "--tail", "30", id)
		if logErr == nil && len(bytes.TrimSpace(logs)) != 0 {
			return fmt.Errorf("peer container exited with code %d: %s", code, bytes.TrimSpace(logs))
		}
		return fmt.Errorf("peer container exited with code %d", code)
	}
	return nil
}

func (d *dockerRuntime) remove(ctx context.Context, id string) error {
	if !dockerContainerID.MatchString(id) && !dockerPeerName.MatchString(id) {
		return fmt.Errorf("refusing to remove invalid peer container ID or name")
	}
	output, err := d.run(ctx, nil, "rm", "--force", id)
	if err != nil && !strings.Contains(strings.ToLower(string(output)), "no such container:") {
		return fmt.Errorf("remove peer container %s: %w", id, err)
	}
	return nil
}

// removeAgentContainers reclaims peers left behind by an Agent crash. Agent IDs
// must be unique for all Agents using the same Docker daemon and peer network.
func (d *dockerRuntime) removeAgentContainers(ctx context.Context, agentID string) error {
	if agentID == "" {
		return fmt.Errorf("agent ID is required for Docker reconciliation")
	}
	output, err := d.run(ctx, nil, "ps", "--all", "--quiet", "--no-trunc",
		"--filter", "label="+managedContainerLabel, "--filter", "label=io.kpl.agent="+agentID,
		"--filter", "label=io.kpl.network="+d.network)
	if err != nil {
		return fmt.Errorf("list previous Agent containers: %w", err)
	}
	var failures []error
	for _, id := range strings.Fields(string(output)) {
		if err := d.remove(ctx, id); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

func peerContainerName(node model.Node) (string, error) {
	identity, err := json.Marshal([]any{node.AgentID, node.RunID, node.ID, node.Generation})
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(identity)
	var attempt [6]byte
	if _, err := rand.Read(attempt[:]); err != nil {
		return "", fmt.Errorf("generate peer container name: %w", err)
	}
	return fmt.Sprintf("kpl-peer-%x-%x", digest[:10], attempt), nil
}

func peerConfigArchive(data []byte) ([]byte, error) {
	var buffer bytes.Buffer
	w := tar.NewWriter(&buffer)
	if err := w.WriteHeader(&tar.Header{Name: strings.TrimPrefix(peerContainerConfig, "/"), Mode: 0600, Size: int64(len(data))}); err != nil {
		return nil, fmt.Errorf("prepare peer config archive: %w", err)
	}
	if _, err := w.Write(data); err != nil {
		return nil, fmt.Errorf("write peer config archive: %w", err)
	}
	if err := w.Close(); err != nil {
		return nil, fmt.Errorf("close peer config archive: %w", err)
	}
	return buffer.Bytes(), nil
}

// CLI output is bounded so a noisy Docker daemon or peer cannot grow the Agent
// indefinitely. The command still drains output after the limit is reached.
type limitedDockerOutput struct{ bytes.Buffer }

func (b *limitedDockerOutput) Write(data []byte) (int, error) {
	n := len(data)
	if remaining := 32*1024 - b.Len(); remaining > 0 {
		if len(data) > remaining {
			data = data[:remaining]
		}
		_, _ = b.Buffer.Write(data)
	}
	return n, nil
}

func (d *dockerRuntime) run(ctx context.Context, input io.Reader, args ...string) ([]byte, error) {
	commandContext := d.commandContext
	if commandContext == nil {
		commandContext = exec.CommandContext
	}
	command := commandContext(ctx, d.binary, args...)
	command.Stdin = input
	var output, stderr limitedDockerOutput
	command.Stdout = &output
	command.Stderr = &stderr
	err := command.Run()
	if ctx.Err() != nil {
		err = ctx.Err()
	}
	if err != nil {
		combined := append(output.Bytes(), stderr.Bytes()...)
		return combined, fmt.Errorf("docker %s: %w: %s", args[0], err, strings.TrimSpace(string(combined)))
	}
	// Docker logs forwards the container's stderr there even on success. For
	// machine-readable commands, keep CLI warnings separate from stdout JSON/IDs.
	if args[0] == "logs" {
		return append(output.Bytes(), stderr.Bytes()...), nil
	}
	return output.Bytes(), nil
}
