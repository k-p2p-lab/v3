package peer

import (
	"encoding/hex"
	"time"

	"github.com/k-p2p-lab/v3/internal/model"
	pubsubpb "github.com/libp2p/go-libp2p-pubsub/pb"
	"github.com/libp2p/go-libp2p/core/peer"
)

type gossipTracer struct {
	telemetry *telemetry
	peerID    string
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
		if event.DuplicateMessage == nil {
			return
		}
		trace.Type = "duplicate"
		trace.MessageID = hex.EncodeToString(event.DuplicateMessage.MessageID)
		trace.Topic = event.DuplicateMessage.GetTopic()
		trace.RemotePeerID = peerID(event.DuplicateMessage.ReceivedFrom)
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

func peerID(raw []byte) string {
	id, err := peer.IDFromBytes(raw)
	if err != nil {
		return ""
	}
	return id.String()
}
