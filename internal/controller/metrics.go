package controller

import (
	"encoding/json"
	"math"
	"strings"
	"sync"

	"github.com/k-p2p-lab/v3/internal/model"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
)

// Event metrics accumulate at ingestion rather than from the bounded UI event
// history. Each Controller has an isolated registry; counters reset on restart.
type controllerMetrics struct {
	registry          *prometheus.Registry
	events            *prometheus.CounterVec
	messageBytes      *prometheus.CounterVec
	propagation       *prometheus.HistogramVec
	operationFailures *prometheus.CounterVec
	droppedEvents     *prometheus.CounterVec
	initializedAgents sync.Map
	initializedTopics sync.Map
}

func newControllerMetrics(s *state) *controllerMetrics {
	m := &controllerMetrics{
		registry: prometheus.NewRegistry(),
		events: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "kpl_events_total", Help: "Telemetry and Controller events received since Controller startup.",
		}, []string{"run_id", "agent_id", "event_type", "topic"}),
		messageBytes: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "kpl_message_bytes_total", Help: "PubSub data bytes reported by publish and deliver events; excludes libp2p transport framing.",
		}, []string{"run_id", "agent_id", "topic", "direction", "encoding"}),
		propagation: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name: "kpl_propagation_latency_seconds", Help: "Nonnegative envelope delivery latency in seconds; excludes raw payloads and unavailable timestamps.",
			Buckets: []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60},
		}, []string{"run_id", "agent_id", "topic"}),
		operationFailures: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "kpl_operation_failures_total", Help: "Phase operation failures recorded by the continue policy.",
		}, []string{"run_id", "agent_id", "action"}),
		droppedEvents: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "kpl_telemetry_dropped_events_total", Help: "Peer telemetry queue drops reported to the Controller.",
		}, []string{"run_id", "agent_id"}),
	}
	m.registry.MustRegister(m.events, m.messageBytes, m.propagation, m.operationFailures, m.droppedEvents,
		newControllerStateCollector(s), newNetworkCollector(s), collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	return m
}

// Known peers establish zero-valued series before traffic where possible, so a
// scrape between the heartbeat and publication can provide a rate baseline.
// Initialization is shared by run/Agent/topic rather than by individual node.
func (m *controllerMetrics) initNode(node model.Node) {
	if node.RunID == "" || node.AgentID == "" {
		return
	}
	if _, initialized := m.initializedAgents.LoadOrStore([2]string{node.RunID, node.AgentID}, struct{}{}); !initialized {
		// Connection lifecycle events have no topic. These series must exist
		// before disconnects to expose their first increase to Prometheus.
		for _, eventType := range []string{"add_peer", "remove_peer"} {
			m.events.WithLabelValues(node.RunID, node.AgentID, eventType, "")
		}
	}
	topics := nodeTopics(node)
	if len(topics) == 0 {
		topics = []string{"kpl/default"}
	}
	for _, topic := range topics {
		topic = strings.TrimSpace(topic)
		if topic == "" {
			continue
		}
		key := [3]string{node.RunID, node.AgentID, topic}
		if _, initialized := m.initializedTopics.LoadOrStore(key, struct{}{}); initialized {
			continue
		}
		for _, eventType := range []string{"publish", "deliver", "duplicate", "graft", "prune", "join", "leave"} {
			m.events.WithLabelValues(node.RunID, node.AgentID, eventType, topic)
		}
		for _, direction := range []string{"publish", "deliver"} {
			for _, encoding := range []string{"envelope", "raw"} {
				m.messageBytes.WithLabelValues(node.RunID, node.AgentID, topic, direction, encoding)
			}
		}
		m.propagation.WithLabelValues(node.RunID, node.AgentID, topic)
		for _, action := range []string{"publish", "leave"} {
			m.operationFailures.WithLabelValues(node.RunID, node.AgentID, action)
		}
		m.droppedEvents.WithLabelValues(node.RunID, node.AgentID)
	}
}

func (m *controllerMetrics) observeEvent(event model.TraceEvent) {
	m.events.WithLabelValues(event.RunID, event.AgentID, event.Type, event.Topic).Inc()
	switch event.Type {
	case "publish", "deliver":
		encoding, _ := event.Fields["payloadEncoding"].(string)
		if encoding != "envelope" && encoding != "raw" {
			encoding = "unknown"
		}
		if wireBytes, ok := nonnegativeMetricNumber(event.Fields["wireBytes"]); ok {
			m.messageBytes.WithLabelValues(event.RunID, event.AgentID, event.Topic, event.Type, encoding).Add(wireBytes)
		}
		available, _ := event.Fields["latencyAvailable"].(bool)
		if event.Type == "deliver" && encoding == "envelope" && available && event.LatencyMS >= 0 && !math.IsNaN(event.LatencyMS) && !math.IsInf(event.LatencyMS, 0) {
			m.propagation.WithLabelValues(event.RunID, event.AgentID, event.Topic).Observe(event.LatencyMS / 1000)
		}
	case "phase-operation-failed":
		action, _ := event.Fields["action"].(string)
		m.operationFailures.WithLabelValues(event.RunID, event.AgentID, action).Inc()
	case "telemetry_drop":
		if count, ok := nonnegativeMetricNumber(event.Fields["count"]); ok {
			m.droppedEvents.WithLabelValues(event.RunID, event.AgentID).Add(count)
		}
	}
}

func nonnegativeMetricNumber(value any) (float64, bool) {
	var number float64
	switch value := value.(type) {
	case float64:
		number = value
	case float32:
		number = float64(value)
	case int:
		number = float64(value)
	case int64:
		number = float64(value)
	case int32:
		number = float64(value)
	case uint:
		number = float64(value)
	case uint64:
		number = float64(value)
	case uint32:
		number = float64(value)
	case json.Number:
		var err error
		number, err = value.Float64()
		if err != nil {
			return 0, false
		}
	default:
		return 0, false
	}
	return number, number >= 0 && !math.IsNaN(number) && !math.IsInf(number, 0)
}

type controllerStateCollector struct {
	state           *state
	agentUp         *prometheus.Desc
	agentCapacity   *prometheus.Desc
	agentActive     *prometheus.Desc
	agentLastSeen   *prometheus.Desc
	nodes           *prometheus.Desc
	experimentInfo  *prometheus.Desc
	experimentPhase *prometheus.Desc
	experimentJobs  *prometheus.Desc
}

func newControllerStateCollector(s *state) *controllerStateCollector {
	return &controllerStateCollector{
		state:           s,
		agentUp:         prometheus.NewDesc("kpl_agent_up", "Whether the Agent is currently online (1) or offline (0).", []string{"agent_id"}, nil),
		agentCapacity:   prometheus.NewDesc("kpl_agent_capacity", "Configured Agent node capacity; zero or negative denotes unlimited capacity.", []string{"agent_id"}, nil),
		agentActive:     prometheus.NewDesc("kpl_agent_active_nodes", "Agent active nodes including pending Controller reservations.", []string{"agent_id"}, nil),
		agentLastSeen:   prometheus.NewDesc("kpl_agent_last_seen_timestamp_seconds", "Unix timestamp of the latest Agent registration or heartbeat.", []string{"agent_id"}, nil),
		nodes:           prometheus.NewDesc("kpl_nodes", "Known nodes grouped by experiment, Agent, group, role, type, and lifecycle state.", []string{"run_id", "agent_id", "group", "role", "node_type", "state"}, nil),
		experimentInfo:  prometheus.NewDesc("kpl_experiment_info", "Experiment information with its current state.", []string{"run_id", "name", "state"}, nil),
		experimentPhase: prometheus.NewDesc("kpl_experiment_phase", "Current experiment phase index as reported by the Controller.", []string{"run_id"}, nil),
		experimentJobs:  prometheus.NewDesc("kpl_experiment_jobs", "Background jobs grouped by experiment and current job state.", []string{"run_id", "state"}, nil),
	}
}

func (c *controllerStateCollector) Describe(ch chan<- *prometheus.Desc) {
	for _, desc := range []*prometheus.Desc{c.agentUp, c.agentCapacity, c.agentActive, c.agentLastSeen, c.nodes, c.experimentInfo, c.experimentPhase, c.experimentJobs} {
		ch <- desc
	}
}

type nodeMetricLabels struct {
	runID, agentID, group, role, nodeType, state string
}

func (c *controllerStateCollector) Collect(ch chan<- prometheus.Metric) {
	// Copy only required state under the lock. In particular, do not call
	// snapshot(), which sorts events and derives UI reachability and latency.
	c.state.mu.RLock()
	agents := make([]model.Agent, 0, len(c.state.agents))
	for _, agent := range c.state.agents {
		agents = append(agents, agent)
	}
	nodes := make([]nodeMetricLabels, 0, len(c.state.nodes))
	for _, node := range c.state.nodes {
		nodes = append(nodes, nodeMetricLabels{node.RunID, node.AgentID, node.Group, node.Role, node.Type, node.State})
	}
	experiments := make([]model.Experiment, 0, len(c.state.experiments))
	for _, experiment := range c.state.experiments {
		experiments = append(experiments, experiment)
	}
	c.state.mu.RUnlock()

	for _, agent := range agents {
		up := 0.0
		if agent.State == model.AgentOnline {
			up = 1
		}
		lastSeen := 0.0
		if !agent.LastSeen.IsZero() {
			lastSeen = float64(agent.LastSeen.UnixNano()) / 1e9
		}
		ch <- prometheus.MustNewConstMetric(c.agentUp, prometheus.GaugeValue, up, agent.ID)
		ch <- prometheus.MustNewConstMetric(c.agentCapacity, prometheus.GaugeValue, float64(agent.Capacity), agent.ID)
		ch <- prometheus.MustNewConstMetric(c.agentActive, prometheus.GaugeValue, float64(agent.ActiveNodes), agent.ID)
		ch <- prometheus.MustNewConstMetric(c.agentLastSeen, prometheus.GaugeValue, lastSeen, agent.ID)
	}
	counts := make(map[nodeMetricLabels]int, len(nodes))
	for _, node := range nodes {
		counts[node]++
	}
	for labels, count := range counts {
		ch <- prometheus.MustNewConstMetric(c.nodes, prometheus.GaugeValue, float64(count), labels.runID, labels.agentID, labels.group, labels.role, labels.nodeType, labels.state)
	}
	for _, experiment := range experiments {
		ch <- prometheus.MustNewConstMetric(c.experimentInfo, prometheus.GaugeValue, 1, experiment.ID, experiment.Name, experiment.State)
		ch <- prometheus.MustNewConstMetric(c.experimentPhase, prometheus.GaugeValue, float64(experiment.Phase), experiment.ID)
		for state, count := range map[string]int{"active": experiment.ActiveJobs, "completed": experiment.CompletedJobs, "failed": experiment.FailedJobs, "canceled": experiment.CanceledJobs} {
			ch <- prometheus.MustNewConstMetric(c.experimentJobs, prometheus.GaugeValue, float64(count), experiment.ID, state)
		}
	}
}
