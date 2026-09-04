package peer

import (
	crand "crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/k-p2p-lab/v3/internal/model"
)

type publication struct {
	wire     []byte
	id       string
	encoding string
	sentAt   time.Time
}

func (s *Server) publishTopics(requested string) ([]string, error) {
	if requested == "" {
		requested = s.config.NodeConfig.GossipSub.Topics[0]
	}
	if requested != "*" {
		if _, ok := s.topics[requested]; !ok {
			return nil, fmt.Errorf("unknown topic %q", requested)
		}
		return []string{requested}, nil
	}
	// The topic map contains only topics that this peer actually joined.
	topics := make([]string, 0, len(s.topics))
	for topic := range s.topics {
		topics = append(topics, topic)
	}
	sort.Strings(topics)
	if len(topics) == 0 {
		return nil, fmt.Errorf("peer has no joined topics")
	}
	return topics, nil
}

func (s *Server) preparePublication(request model.PublishRequest, now time.Time) (publication, error) {
	encoding := request.PayloadEncoding
	if encoding == "" {
		encoding = "envelope"
	}
	if encoding != "envelope" && encoding != "raw" {
		return publication{}, fmt.Errorf("payloadEncoding must be envelope or raw")
	}
	payload := make([]byte, request.PayloadSize)
	if _, err := crand.Read(payload); err != nil {
		return publication{}, fmt.Errorf("generate payload: %w", err)
	}
	if encoding == "raw" {
		// No application header or base64 expansion: exactly payloadSize bytes
		// are passed to Topic.Publish. Libp2p still adds its own framing/signature.
		return publication{wire: payload, id: rawMessageID(payload), encoding: encoding, sentAt: now}, nil
	}
	sequence := s.publishSeq.Add(1)
	digest := sha256.Sum256([]byte(fmt.Sprintf("%s:%d:%d", s.config.Node.ID, now.UnixNano(), sequence)))
	message := envelope{ID: hex.EncodeToString(digest[:16]), RunID: s.config.Node.RunID, Publisher: s.config.Node.ID, SentAt: now.UnixNano(), Payload: payload}
	wire, err := json.Marshal(message)
	if err != nil {
		return publication{}, fmt.Errorf("encode message: %w", err)
	}
	return publication{wire: wire, id: message.ID, encoding: encoding, sentAt: now}, nil
}

func rawMessageID(data []byte) string {
	digest := sha256.Sum256(data)
	return "raw-" + hex.EncodeToString(digest[:])
}

func (s *Server) deliveryEvent(data []byte, topic, receivedFrom string, now time.Time) (model.TraceEvent, bool) {
	event := model.TraceEvent{PeerID: s.host.ID().String(), Type: "deliver", Topic: topic, RemotePeerID: receivedFrom, Timestamp: now}
	var payload envelope
	if json.Unmarshal(data, &payload) == nil && payload.ID != "" && payload.RunID != "" && payload.Publisher != "" && payload.SentAt > 0 {
		if payload.RunID != s.config.Node.RunID {
			return model.TraceEvent{}, false
		}
		event.MessageID = payload.ID
		event.LatencyMS = float64(now.UnixNano()-payload.SentAt) / float64(time.Millisecond)
		event.Fields = map[string]any{"publisher": payload.Publisher, "payloadBytes": len(payload.Payload), "wireBytes": len(data), "payloadEncoding": "envelope", "latencyAvailable": true}
		if event.LatencyMS < 0 {
			event.Fields["clockSkewDetected"] = true
		}
		return event, true
	}
	// Raw bytes intentionally contain no publisher, run ID, or sent timestamp.
	// Hashing the data correlates received content with publish events without
	// pretending that one-way latency or embedded run isolation is available.
	event.MessageID = rawMessageID(data)
	event.LatencyMS = -1
	event.Fields = map[string]any{"payloadBytes": len(data), "wireBytes": len(data), "payloadEncoding": "raw", "latencyAvailable": false}
	return event, true
}
