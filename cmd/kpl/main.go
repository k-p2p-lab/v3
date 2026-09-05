package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"github.com/k-p2p-lab/v3/internal/agent"
	"github.com/k-p2p-lab/v3/internal/controller"
	"github.com/k-p2p-lab/v3/internal/peer"
	"github.com/k-p2p-lab/v3/internal/scenario"
)

const version = "v3-dev"

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "controller":
		err = runController(ctx, logger, os.Args[2:])
	case "agent":
		err = runAgent(ctx, logger, os.Args[2:])
	case "peer":
		err = runPeer(ctx, logger, os.Args[2:])
	case "validate":
		err = validateScenario(os.Args[2:])
	case "version", "--version", "-version":
		fmt.Println(version)
		return
	case "help", "--help", "-h":
		usage()
		return
	default:
		usage()
		os.Exit(2)
	}
	if err != nil {
		logger.Error("kpl stopped", "role", os.Args[1], "error", err)
		os.Exit(1)
	}
}

func runController(ctx context.Context, logger *slog.Logger, args []string) error {
	flags := flag.NewFlagSet("controller", flag.ContinueOnError)
	listen := flags.String("listen", ":8080", "HTTP listen address")
	dataDir := flags.String("data-dir", "data", "experiment data directory")
	token := flags.String("token", os.Getenv("KPL_API_TOKEN"), "optional shared API token")
	prometheusPort := flags.String("prometheus-port", os.Getenv("PROMETHEUS_PORT"), "public Prometheus port advertised to the Dashboard (default 9090)")
	grafanaPort := flags.String("grafana-port", os.Getenv("GRAFANA_PORT"), "public Grafana port advertised to the Dashboard (default 3000)")
	if err := flags.Parse(args); err != nil {
		return err
	}
	parsedPrometheusPort, err := parsePort(*prometheusPort, 9090, "Prometheus")
	if err != nil {
		return err
	}
	parsedGrafanaPort, err := parsePort(*grafanaPort, 3000, "Grafana")
	if err != nil {
		return err
	}
	server := controller.New(controller.ServerConfig{
		Listen:         *listen,
		DataDir:        *dataDir,
		Token:          *token,
		PrometheusPort: parsedPrometheusPort,
		GrafanaPort:    parsedGrafanaPort,
	}, logger)
	return server.Run(ctx)
}

func parsePort(raw string, fallback int, name string) (int, error) {
	if strings.TrimSpace(raw) == "" {
		return fallback, nil
	}
	port, err := strconv.Atoi(raw)
	if err != nil || port < 1 || port > 65535 {
		return 0, fmt.Errorf("%s port must be an integer from 1 to 65535", name)
	}
	return port, nil
}

func runAgent(ctx context.Context, logger *slog.Logger, args []string) error {
	flags := flag.NewFlagSet("agent", flag.ContinueOnError)
	id := flags.String("id", "", "stable unique agent id")
	name := flags.String("name", "", "human-readable agent name")
	listen := flags.String("listen", ":8090", "HTTP listen address")
	advertiseURL := flags.String("advertise-url", "", "controller-reachable agent URL")
	selfURL := flags.String("self-url", "", "agent URL reachable from peer containers (defaults to advertise-url in Docker mode)")
	controllerURL := flags.String("controller-url", "http://127.0.0.1:8080", "controller URL")
	capacity := flags.Int("capacity", 100, "maximum active peers")
	dataDir := flags.String("data-dir", "data-agent", "agent data directory")
	token := flags.String("token", os.Getenv("KPL_API_TOKEN"), "optional shared API token")
	peerAPIPort := flags.Int("peer-api-port", 18000, "first peer control API port")
	peerP2PPort := flags.Int("peer-p2p-port", 20000, "first peer libp2p port")
	labels := flags.String("labels", "", "comma-separated key=value labels")
	runtime := flags.String("runtime", "docker", "peer runtime: docker or process")
	dockerBinary := flags.String("docker-binary", "docker", "Docker CLI executable")
	dockerImage := flags.String("docker-image", "kpl-v3:local", "local Docker image for peer containers")
	dockerNetwork := flags.String("docker-network", "kpl-v3-peers", "user-defined bridge or attachable overlay network")
	if err := flags.Parse(args); err != nil {
		return err
	}
	server, err := agent.New(agent.Config{
		ID: *id, Name: *name, Listen: *listen, AdvertiseURL: *advertiseURL, SelfURL: *selfURL,
		ControllerURL: *controllerURL, Capacity: *capacity, DataDir: *dataDir, Token: *token,
		PeerAPIPort: *peerAPIPort, PeerP2PPort: *peerP2PPort, Labels: parseLabels(*labels),
		Runtime: *runtime, DockerBinary: *dockerBinary, DockerImage: *dockerImage, DockerNetwork: *dockerNetwork,
	}, logger)
	if err != nil {
		return err
	}
	return server.Run(ctx)
}

func runPeer(ctx context.Context, logger *slog.Logger, args []string) error {
	flags := flag.NewFlagSet("peer", flag.ContinueOnError)
	configPath := flags.String("config", "", "peer process JSON config")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *configPath == "" {
		return fmt.Errorf("--config is required")
	}
	return peer.Run(ctx, *configPath, logger)
}

func validateScenario(args []string) error {
	flags := flag.NewFlagSet("validate", flag.ContinueOnError)
	path := flags.String("scenario", "", "scenario YAML file")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *path == "" {
		return fmt.Errorf("--scenario is required")
	}
	data, err := os.ReadFile(*path)
	if err != nil {
		return err
	}
	parsed, err := scenario.Parse(data)
	if err != nil {
		return err
	}
	fmt.Printf("valid scenario: %s (%d phases, seed %d)\n", parsed.Name, len(parsed.Phases), parsed.Seed)
	return nil
}

func parseLabels(raw string) map[string]string {
	result := make(map[string]string)
	for _, part := range strings.Split(raw, ",") {
		key, value, ok := strings.Cut(strings.TrimSpace(part), "=")
		if ok && key != "" {
			result[key] = value
		}
	}
	return result
}

func usage() {
	fmt.Fprintln(os.Stderr, `K-P2PLab v3

Usage:
  kpl controller [--listen :8080] [--data-dir data]
  kpl agent --id ID --advertise-url URL [--controller-url URL]
  kpl peer --config FILE
  kpl validate --scenario FILE
  kpl version`)
}
