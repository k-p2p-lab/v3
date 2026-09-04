package peer

import (
	"time"

	"github.com/k-p2p-lab/v3/internal/model"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	pubsubpb "github.com/libp2p/go-libp2p-pubsub/pb"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
)

type gossipTracer struct {
	telemetry  *telemetry
	peerID     string
	publishing *publicationBridge
}

func (t *gossipTracer) Trace(event *pubsubpb.TraceEvent) {
	if event == nil || event.Type == nil {
		return
	}
	trace := model.TraceEvent{PeerID: t.peerID, Timestamp: time.Now().UTC()}
	if event.GetTimestamp() > 0 {
		trace.Timestamp = time.Unix(0, event.GetTimestamp()).UTC()
	}
	switch event.GetType() {
	case pubsubpb.TraceEvent_PUBLISH_MESSAGE:
		if event.PublishMessage != nil {
			t.publishing.record(event.PublishMessage.GetTopic(), event.PublishMessage.MessageID)
		}
		return
	case pubsubpb.TraceEvent_ADD_PEER:
		if event.AddPeer == nil {
			return
		}
		trace.Type = "add_peer"
		trace.RemotePeerID = peerID(event.AddPeer.PeerID)
		trace.Fields = map[string]any{"protocol": event.AddPeer.GetProto()}
	case pubsubpb.TraceEvent_REMOVE_PEER:
		if event.RemovePeer == nil {
			return
		}
		trace.Type = "remove_peer"
		trace.RemotePeerID = peerID(event.RemovePeer.PeerID)
	case pubsubpb.TraceEvent_JOIN:
		if event.Join == nil {
			return
		}
		trace.Type = "join"
		trace.Topic = event.Join.GetTopic()
	case pubsubpb.TraceEvent_LEAVE:
		if event.Leave == nil {
			return
		}
		trace.Type = "leave"
		trace.Topic = event.Leave.GetTopic()
	case pubsubpb.TraceEvent_DUPLICATE_MESSAGE:
		// RawTracer has the original bytes needed to correlate the duplicate
		// with the application's publication. Counting both would double it.
		return
	case pubsubpb.TraceEvent_GRAFT:
		if event.Graft == nil {
			return
		}
		trace.Type = "graft"
		trace.Topic = event.Graft.GetTopic()
		trace.RemotePeerID = peerID(event.Graft.PeerID)
	case pubsubpb.TraceEvent_PRUNE:
		if event.Prune == nil {
			return
		}
		trace.Type = "prune"
		trace.Topic = event.Prune.GetTopic()
		trace.RemotePeerID = peerID(event.Prune.PeerID)
	default:
		return
	}
	t.telemetry.emit(trace)
}

// messageTracer observes duplicate copies without changing validation, routing,
// or PubSub message IDs. Application deliveries are emitted by consume only
// after Subscription.Next succeeds, rather than before its queue accepts data.
type messageTracer struct {
	server *Server
}

var _ pubsub.RawTracer = (*messageTracer)(nil)

func (t *messageTracer) DuplicateMessage(message *pubsub.Message) {
	if message == nil || message.Message == nil {
		return
	}
	event, ok := t.server.duplicateEvent(message, time.Now().UTC())
	if !ok {
		return
	}
	t.server.telemetry.emit(event)
}

func (*messageTracer) AddPeer(peer.ID, protocol.ID)          {}
func (*messageTracer) RemovePeer(peer.ID)                    {}
func (*messageTracer) Join(string)                           {}
func (*messageTracer) Leave(string)                          {}
func (*messageTracer) Graft(peer.ID, string)                 {}
func (*messageTracer) Prune(peer.ID, string)                 {}
func (*messageTracer) ValidateMessage(*pubsub.Message)       {}
func (*messageTracer) DeliverMessage(*pubsub.Message)        {}
func (*messageTracer) RejectMessage(*pubsub.Message, string) {}
func (*messageTracer) ThrottlePeer(peer.ID)                  {}
func (*messageTracer) RecvRPC(*pubsub.RPC)                   {}
func (*messageTracer) SendRPC(*pubsub.RPC, peer.ID)          {}
func (*messageTracer) DropRPC(*pubsub.RPC, peer.ID)          {}
func (*messageTracer) UndeliverableMessage(*pubsub.Message)  {}

func peerID(raw []byte) string {
	id, err := peer.IDFromBytes(raw)
	if err != nil {
		return ""
	}
	return id.String()
}
