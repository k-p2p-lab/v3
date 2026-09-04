package controller

import (
	"testing"
	"time"

	"github.com/k-p2p-lab/v3/internal/model"
)

func TestNetworkEdgesAreDeduplicated(t *testing.T) {
	nodes := []model.Node{
		{ID: "a", PeerID: "peer-a", State: model.NodeReady, ConnectedPeers: []string{"peer-b"}},
		{ID: "b", PeerID: "peer-b", State: model.NodeReady, ConnectedPeers: []string{"peer-a"}},
	}
	edges := networkEdges(nodes)
	if len(edges) != 1 || edges[0].Source != "a" || edges[0].Target != "b" {
		t.Fatalf("unexpected edges: %+v", edges)
	}
}

func TestCalculateMetrics(t *testing.T) {
	experiments := []model.Experiment{{ID: "run-1", StartedAt: time.Now()}}
	nodes := []model.Node{{ID: "a", RunID: "run-1", State: model.NodeReady}, {ID: "b", RunID: "run-1", State: model.NodeReady}}
	events := []model.TraceEvent{
		{RunID: "run-1", NodeID: "b", Type: "deliver", MessageID: "m1", LatencyMS: 10, Timestamp: time.Unix(2, 0)},
		{RunID: "run-1", NodeID: "a", Type: "publish", MessageID: "m1", Timestamp: time.Unix(1, 0)},
		{RunID: "run-1", NodeID: "b", Type: "duplicate", MessageID: "m1", Timestamp: time.Unix(3, 0)},
	}
	metrics := calculateMetrics(nodes, experiments, events)
	if metrics.Published != 1 || metrics.Delivered != 1 || metrics.Duplicates != 1 {
		t.Fatalf("unexpected counters: %+v", metrics)
	}
	if metrics.Reachability != 1 || metrics.P95LatencyMS != 10 {
		t.Fatalf("unexpected propagation metrics: %+v", metrics)
	}
}

func TestRawDeliveryBeforePublisherClockPreservesReachWithoutLatency(t *testing.T) {
	nodes := []model.Node{{ID: "a", RunID: "run"}, {ID: "b", RunID: "run"}}
	events := []model.TraceEvent{
		{RunID: "run", NodeID: "b", Type: "deliver", MessageID: "raw-hash", LatencyMS: -1, Timestamp: time.Unix(1, 0)},
		{RunID: "run", NodeID: "a", Type: "publish", MessageID: "raw-hash", Timestamp: time.Unix(2, 0)},
	}
	metrics := calculateMetrics(nodes, []model.Experiment{{ID: "run"}}, events)
	if metrics.Reachability != 1 || metrics.Delivered != 1 || metrics.AverageLatencyMS != 0 || metrics.P95LatencyMS != 0 {
		t.Fatalf("clock skew lost raw delivery or fabricated latency: %+v", metrics)
	}
}
