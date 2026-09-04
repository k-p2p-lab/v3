package agent

import (
	"net/http"

	"github.com/k-p2p-lab/v3/internal/model"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func (s *Server) metricsHandler() http.Handler {
	return promhttp.HandlerFor(newAgentMetricsRegistry(s), promhttp.HandlerOpts{})
}

func newAgentMetricsRegistry(s *Server) *prometheus.Registry {
	registry := prometheus.NewRegistry()
	registry.MustRegister(newLocalCollector(s))
	// These describe the Agent's own Go runtime and OS process. Peer containers
	// have separate processes and are not included in these CPU/memory metrics.
	processRegistry := prometheus.WrapRegistererWith(prometheus.Labels{"agent_id": s.config.ID}, registry)
	processRegistry.MustRegister(collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	return registry
}

type localCollector struct {
	server                                          *Server
	nodes, capacity, cleanupPending, telemetryQueue *prometheus.Desc
}

func newLocalCollector(s *Server) *localCollector {
	return &localCollector{
		server:         s,
		nodes:          prometheus.NewDesc("kpl_local_nodes", "Peer records held by this Agent, including retained stopped and failed records, by current state and peer runtime.", []string{"agent_id", "state", "runtime"}, nil),
		capacity:       prometheus.NewDesc("kpl_local_capacity", "Configured peer admission capacity of this Agent; this is not a CPU or memory limit.", []string{"agent_id"}, nil),
		cleanupPending: prometheus.NewDesc("kpl_local_cleanup_pending", "Docker peers awaiting cleanup or whose previous container cleanup failed.", []string{"agent_id"}, nil),
		telemetryQueue: prometheus.NewDesc("kpl_local_telemetry_queue_events", "Telemetry events currently queued in this Agent for forwarding to the Controller.", []string{"agent_id"}, nil),
	}
}

func (c *localCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.nodes
	ch <- c.capacity
	ch <- c.cleanupPending
	ch <- c.telemetryQueue
}

func (c *localCollector) Collect(ch chan<- prometheus.Metric) {
	type nodeKey struct{ state, runtime string }
	s := c.server
	s.mu.RLock()
	agentID, capacity := s.config.ID, s.config.Capacity
	defaultRuntime := metricsRuntime(s.config.Runtime)
	counts := make(map[nodeKey]int)
	for _, state := range []string{model.NodeStarting, model.NodeReady, model.NodeStopping, model.NodeStopped, model.NodeFailed} {
		counts[nodeKey{state, defaultRuntime}] = 0
	}
	cleanupPending := 0
	for _, proc := range s.processes {
		runtime := proc.node.Metadata["runtime"]
		if runtime == "" {
			runtime = defaultRuntime
		}
		runtime = metricsRuntime(runtime)
		counts[nodeKey{metricsNodeState(proc.node.State), runtime}]++
		if runtime == "docker" && (proc.cleanupErr != nil || !proc.exited && proc.node.State == model.NodeStopping) {
			cleanupPending++
		}
	}
	s.mu.RUnlock()
	s.eventsMu.Lock()
	queued := len(s.events)
	s.eventsMu.Unlock()
	for key, count := range counts {
		ch <- prometheus.MustNewConstMetric(c.nodes, prometheus.GaugeValue, float64(count), agentID, key.state, key.runtime)
	}
	ch <- prometheus.MustNewConstMetric(c.capacity, prometheus.GaugeValue, float64(capacity), agentID)
	ch <- prometheus.MustNewConstMetric(c.cleanupPending, prometheus.GaugeValue, float64(cleanupPending), agentID)
	ch <- prometheus.MustNewConstMetric(c.telemetryQueue, prometheus.GaugeValue, float64(queued), agentID)
}

func metricsRuntime(value string) string {
	if value == "docker" || value == "process" {
		return value
	}
	return "unknown"
}

func metricsNodeState(value string) string {
	switch value {
	case model.NodeStarting, model.NodeReady, model.NodeStopping, model.NodeStopped, model.NodeFailed:
		return value
	default:
		return "unknown"
	}
}
