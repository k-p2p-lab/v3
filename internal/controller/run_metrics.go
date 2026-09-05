package controller

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"sort"
	"time"

	"github.com/k-p2p-lab/v3/internal/model"
)

type messageMetricKey struct{ topic, id string }
type deliveryMetric struct {
	timestamp            time.Time
	agentID              string
	latencyMS            float64
	latencyAvailable     bool
	clockSynchronized    bool
	latencyUncertaintyMS float64
}
type messageMetric struct {
	published  bool
	publisher  string
	targets    map[string]struct{} // nil: unknown legacy cohort, empty: explicitly none
	deliveries map[string]deliveryMetric
	duplicates map[string]int
}

// This compact per-message index outlives the 300-row recent-events feed.
// All access is protected by state.mu; deleting a result releases its index.
type runMetricAccumulator struct {
	seen                             map[string]struct{}
	messages                         map[messageMetricKey]*messageMetric
	published, delivered, duplicates int
	window                           sessionWindowAccumulator
}

func newRunMetricAccumulator() *runMetricAccumulator {
	return &runMetricAccumulator{seen: make(map[string]struct{}), messages: make(map[messageMetricKey]*messageMetric)}
}

func eventIdentity(event model.TraceEvent) string {
	if event.SessionID != "" && event.Sequence > 0 {
		return fmt.Sprintf("%s\x00session:%s:%d", event.NodeID, event.SessionID, event.Sequence)
	}
	if event.EventID == "" {
		return ""
	}
	return event.NodeID + "\x00" + event.EventID
}

func (a *runMetricAccumulator) hasEvent(event model.TraceEvent) bool {
	id := eventIdentity(event)
	_, exists := a.seen[id]
	return id != "" && exists
}

// observe tolerates delivery/duplicate telemetry arriving before publication.
// Event IDs distinguish actual network duplicates from HTTP batch retries.
func (a *runMetricAccumulator) observe(event model.TraceEvent) bool {
	if a.hasEvent(event) {
		return false
	}
	if id := eventIdentity(event); id != "" {
		a.seen[id] = struct{}{}
	}
	a.window.observe(event)
	if event.Type != "publish" && event.Type != "deliver" && event.Type != "duplicate" {
		return true
	}
	var message *messageMetric
	if event.MessageID != "" {
		key := messageMetricKey{event.Topic, event.MessageID}
		message = a.messages[key]
		if message == nil {
			message = &messageMetric{deliveries: make(map[string]deliveryMetric), duplicates: make(map[string]int)}
			a.messages[key] = message
		}
	}
	switch event.Type {
	case "publish":
		if message == nil {
			a.published++
			return true
		}
		if message.published {
			return true
		}
		a.published++
		message.published = true
		message.publisher = event.NodeID
		if targets, known := targetNodeIDs(event.Fields["targetNodeIds"]); known {
			message.targets = make(map[string]struct{}, len(targets))
			for _, id := range targets {
				if id != "" && id != event.NodeID {
					message.targets[id] = struct{}{}
				}
			}
		}
	case "deliver":
		a.delivered++
		if message == nil || event.NodeID == "" {
			return true
		}
		if local, _ := event.Fields["localDelivery"].(bool); local {
			return true
		}
		previous, exists := message.deliveries[event.NodeID]
		if exists && !event.Timestamp.Before(previous.timestamp) {
			return true
		}
		encoding, _ := event.Fields["payloadEncoding"].(string)
		available, _ := event.Fields["latencyAvailable"].(bool)
		clockSynchronized, _ := event.Fields["latencyClockSynchronized"].(bool)
		latencyUncertaintyMS, _ := numericEventField(event.Fields, "latencyUncertaintyMs")
		message.deliveries[event.NodeID] = deliveryMetric{
			timestamp: event.Timestamp, agentID: event.AgentID, latencyMS: event.LatencyMS,
			latencyAvailable: encoding == "envelope" && available, clockSynchronized: clockSynchronized,
			latencyUncertaintyMS: latencyUncertaintyMS,
		}
	case "duplicate":
		a.duplicates++
		if message != nil && event.NodeID != "" {
			message.duplicates[event.NodeID]++
		}
	}
	return true
}

func numericEventField(fields map[string]any, key string) (float64, bool) {
	value, exists := fields[key]
	if !exists {
		return 0, false
	}
	switch value := value.(type) {
	case float64:
		return value, true
	case float32:
		return float64(value), true
	case int:
		return float64(value), true
	case int64:
		return float64(value), true
	case json.Number:
		parsed, err := value.Float64()
		return parsed, err == nil
	default:
		return 0, false
	}
}

func targetNodeIDs(value any) ([]string, bool) {
	switch value := value.(type) {
	case []string:
		return value, value != nil
	case []any:
		if value == nil {
			return nil, false
		}
		ids := make([]string, 0, len(value))
		for _, item := range value {
			id, ok := item.(string)
			if !ok {
				return nil, false
			}
			ids = append(ids, id)
		}
		return ids, true
	default:
		return nil, false
	}
}

type propagationSeriesKey struct{ agentID, topic string }
type propagationSample struct {
	key     propagationSeriesKey
	seconds float64
}

func (a *runMetricAccumulator) summarize(runID string, asOf ...time.Time) (model.Metrics, []propagationSample) {
	if a.window.enabled {
		boundary := time.Now().UTC()
		if len(asOf) > 0 {
			boundary = asOf[0]
		}
		return a.window.summarize(runID, boundary, a.published, a.delivered, a.duplicates)
	}
	return a.summarizeLegacy(runID)
}

func (a *runMetricAccumulator) summarizeLegacy(runID string) (model.Metrics, []propagationSample) {
	result := model.Metrics{Definition: "dispatch-cohort-v1", RunID: runID, Published: a.published, Delivered: a.delivered, Duplicates: a.duplicates}
	latencies := make([]float64, 0)
	samples := make([]propagationSample, 0)
	knownPublications := 0
	for key, message := range a.messages {
		if !message.published || message.targets == nil {
			continue
		}
		knownPublications++
		result.ExpectedDeliveries += len(message.targets)
		for receiver := range message.targets {
			delivery, exists := message.deliveries[receiver]
			if !exists || receiver == message.publisher {
				continue
			}
			result.EligibleDeliveries++
			result.EligibleDuplicates += message.duplicates[receiver]
			if delivery.latencyAvailable {
				latency := delivery.latencyMS
				if latency < 0 || math.IsNaN(latency) || math.IsInf(latency, 0) {
					result.InvalidLatencySamples++
					continue
				}
				latencies = append(latencies, latency)
				samples = append(samples, propagationSample{propagationSeriesKey{delivery.agentID, key.topic}, latency / 1000})
			}
		}
	}
	result.UnscopedPublications = result.Published - knownPublications
	result.DeliveryRatioAvailable = result.ExpectedDeliveries > 0
	if result.DeliveryRatioAvailable {
		result.Reachability = float64(result.EligibleDeliveries) / float64(result.ExpectedDeliveries)
	}
	result.DuplicateSamples = result.EligibleDeliveries
	if result.DuplicateSamples > 0 {
		result.AverageDuplicates = float64(result.EligibleDuplicates) / float64(result.DuplicateSamples)
	}
	result.LatencySamples = len(latencies)
	if len(latencies) > 0 {
		sort.Float64s(latencies)
		for _, latency := range latencies {
			result.AverageLatencyMS += latency
		}
		result.AverageLatencyMS /= float64(len(latencies))
		result.P95LatencyMS = latencies[int(math.Ceil(0.95*float64(len(latencies))))-1]
	}
	return result, samples
}

// Rebuild the identical metrics from a pinned events.jsonl prefix in a result
// archive. No current topology, Controller uptime, or event arrival order is
// required. Legacy events without a cohort retain counts but have no ratio.
func summarizeRunEvents(runID string, reader io.Reader, asOf ...time.Time) (model.Metrics, error) {
	a := newRunMetricAccumulator()
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	line := 0
	for scanner.Scan() {
		line++
		var event model.TraceEvent
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			return model.Metrics{}, fmt.Errorf("decode event line %d: %w", line, err)
		}
		if event.RunID == runID {
			a.observe(event)
		}
	}
	if err := scanner.Err(); err != nil {
		return model.Metrics{}, fmt.Errorf("read event log: %w", err)
	}
	metrics, _ := a.summarize(runID, asOf...)
	return metrics, nil
}

func calculateMetrics(_ []model.Node, experiments []model.Experiment, events []model.TraceEvent) model.Metrics {
	runID := ""
	if len(experiments) > 0 {
		runID = experiments[0].ID
	}
	a := newRunMetricAccumulator()
	for _, event := range events {
		if runID == "" || event.RunID == runID {
			a.observe(event)
		}
	}
	metrics, _ := a.summarize(runID)
	return metrics
}
