package peer

import (
	"encoding/hex"
	"time"

	"github.com/elecbug/kpl-v3/internal/model"
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
	switch event.GetType() {
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
