package peer

import (
	"sort"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
)

// meshTracker follows synchronous router decisions rather than the lossy event
// queue. In go-libp2p-pubsub, accepted inbound GRAFTs, heartbeat adjustments and
// Join's fanout promotion all call Graft; RemovePeer deletes a peer from every
// topic. Leave precedes its trailing Prune callbacks.
type meshTracker struct {
	mu             sync.RWMutex
	enabled        bool
	topics         map[string]map[peer.ID]struct{}
	lastObservedAt time.Time
}

func (m *meshTracker) configure(enabled bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.enabled = enabled
	m.topics = make(map[string]map[peer.ID]struct{})
}

func (m *meshTracker) join(topic string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.enabled && m.topics[topic] == nil {
		m.topics[topic] = make(map[peer.ID]struct{})
	}
}

func (m *meshTracker) graft(id peer.ID, topic string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.enabled {
		return
	}
	if m.topics[topic] == nil {
		m.topics[topic] = make(map[peer.ID]struct{})
	}
	m.topics[topic][id] = struct{}{}
}

func (m *meshTracker) prune(id peer.ID, topic string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	// Do not recreate a topic removed by Leave, which emits Prune afterwards.
	delete(m.topics[topic], id)
}

func (m *meshTracker) removePeer(id peer.ID) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, peers := range m.topics {
		delete(peers, id)
	}
}

func (m *meshTracker) leave(topic string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.topics, topic)
}

func (m *meshTracker) snapshot() (map[string][]string, time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	topics := make(map[string][]string, len(m.topics))
	if m.enabled {
		for topic, peers := range m.topics {
			ids := make([]string, 0, len(peers))
			for id := range peers {
				ids = append(ids, id.String())
			}
			sort.Strings(ids)
			topics[topic] = ids
		}
	}
	// The timestamp travels with this copy, so delayed status requests can be
	// recognized downstream. Keep it strictly increasing because downstream
	// rejects equal timestamps as duplicate observations, while some system
	// clocks can return the same value for consecutive calls.
	observedAt := time.Now().UTC()
	if !observedAt.After(m.lastObservedAt) {
		observedAt = m.lastObservedAt.Add(time.Nanosecond)
	}
	m.lastObservedAt = observedAt
	return topics, observedAt
}

func (s *Server) overlaySnapshot() ([]string, map[string][]string, time.Time) {
	routing := make([]string, 0)
	if s.dht != nil {
		for _, id := range s.dht.RoutingTable().ListPeers() {
			routing = append(routing, id.String())
		}
		sort.Strings(routing)
	}
	mesh, observedAt := s.mesh.snapshot()
	return routing, mesh, observedAt
}
