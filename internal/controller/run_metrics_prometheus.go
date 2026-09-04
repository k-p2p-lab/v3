package controller

import (
	"github.com/k-p2p-lab/v3/internal/model"
	"github.com/prometheus/client_golang/prometheus"
)

// Reconstructing the histogram from first deliveries also handles out-of-order
// batches: a late publish cohort or an earlier delivery can correct a sample.
// No transport retry, self-delivery, or late join enters the histogram.
type runMetricsCollector struct {
	state                                                              *state
	propagation, expected, delivered, ratio, duplicates, duplicateMean *prometheus.Desc
}

func newRunMetricsCollector(s *state) *runMetricsCollector {
	return &runMetricsCollector{
		state:         s,
		propagation:   prometheus.NewDesc("kpl_propagation_latency_seconds", "Whole-run first remote cohort application latency; excludes raw, self, late join, unknown cohorts and negative clock-skew samples. Late events may correct buckets/sum: query directly, not with rate/increase.", []string{"run_id", "agent_id", "topic"}, nil),
		expected:      prometheus.NewDesc("kpl_delivery_expected_pairs", "Remote subscriber pairs frozen at publish dispatch, including subsequent departures.", []string{"run_id"}, nil),
		delivered:     prometheus.NewDesc("kpl_delivery_reached_pairs", "Distinct first deliveries among the frozen remote subscriber pairs.", []string{"run_id"}, nil),
		ratio:         prometheus.NewDesc("kpl_delivery_ratio", "Reached pairs divided by expected pairs over the observed run; absent when denominator is zero.", []string{"run_id"}, nil),
		duplicates:    prometheus.NewDesc("kpl_delivery_duplicate_copies", "Additional PubSub copies at successfully delivered remote cohort pairs.", []string{"run_id"}, nil),
		duplicateMean: prometheus.NewDesc("kpl_delivery_duplicates_per_reached_pair", "Additional copies divided by successful remote cohort pairs; absent when denominator is zero.", []string{"run_id"}, nil),
	}
}

func (c *runMetricsCollector) Describe(ch chan<- *prometheus.Desc) {
	for _, desc := range []*prometheus.Desc{c.propagation, c.expected, c.delivered, c.ratio, c.duplicates, c.duplicateMean} {
		ch <- desc
	}
}

var propagationBounds = []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60}

type runPropagationKey struct{ runID, agentID, topic string }
type propagationHistogram struct {
	count   uint64
	sum     float64
	buckets map[float64]uint64
}

func (c *runMetricsCollector) Collect(ch chan<- prometheus.Metric) {
	metrics := make([]model.Metrics, 0)
	histograms := make(map[runPropagationKey]*propagationHistogram)
	initHistogram := func(key runPropagationKey) *propagationHistogram {
		if current := histograms[key]; current != nil {
			return current
		}
		current := &propagationHistogram{buckets: make(map[float64]uint64, len(propagationBounds))}
		for _, bound := range propagationBounds {
			current.buckets[bound] = 0
		}
		histograms[key] = current
		return current
	}
	c.state.mu.RLock()
	for runID, accumulator := range c.state.runMetrics {
		if runID == "" {
			continue
		}
		result, samples := accumulator.summarize(runID)
		metrics = append(metrics, result)
		for _, sample := range samples {
			histogram := initHistogram(runPropagationKey{runID, sample.key.agentID, sample.key.topic})
			histogram.count++
			histogram.sum += sample.seconds
			for _, bound := range propagationBounds {
				if sample.seconds <= bound {
					histogram.buckets[bound]++
				}
			}
		}
	}
	for _, node := range c.state.nodes {
		if node.RunID == "" || node.AgentID == "" {
			continue
		}
		if _, known := c.state.experiments[node.RunID]; !known && c.state.runMetrics[node.RunID] == nil {
			continue
		}
		topics := nodeTopics(node)
		if len(topics) == 0 {
			topics = []string{"kpl/default"}
		}
		for _, topic := range topics {
			initHistogram(runPropagationKey{node.RunID, node.AgentID, topic})
		}
	}
	c.state.mu.RUnlock()
	for _, result := range metrics {
		ch <- prometheus.MustNewConstMetric(c.expected, prometheus.GaugeValue, float64(result.ExpectedDeliveries), result.RunID)
		ch <- prometheus.MustNewConstMetric(c.delivered, prometheus.GaugeValue, float64(result.EligibleDeliveries), result.RunID)
		ch <- prometheus.MustNewConstMetric(c.duplicates, prometheus.GaugeValue, float64(result.EligibleDuplicates), result.RunID)
		if result.DeliveryRatioAvailable {
			ch <- prometheus.MustNewConstMetric(c.ratio, prometheus.GaugeValue, result.Reachability, result.RunID)
		}
		if result.DuplicateSamples > 0 {
			ch <- prometheus.MustNewConstMetric(c.duplicateMean, prometheus.GaugeValue, result.AverageDuplicates, result.RunID)
		}
	}
	for key, histogram := range histograms {
		ch <- prometheus.MustNewConstHistogram(c.propagation, histogram.count, histogram.sum, histogram.buckets, key.runID, key.agentID, key.topic)
	}
}
