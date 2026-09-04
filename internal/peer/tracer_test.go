package peer

import (
	"testing"
	"time"

	"github.com/k-p2p-lab/v3/internal/model"
	pubsubpb "github.com/libp2p/go-libp2p-pubsub/pb"
	corepeer "github.com/libp2p/go-libp2p/core/peer"
)

func TestGossipTracerPreservesPeerAndTopicLifecycle(t *testing.T) {
	nodes := bootstrapTestNodes(t, 1)
	remote, err := corepeer.Decode(nodes[0].PeerID)
	if err != nil {
		t.Fatal(err)
	}
	protocol, topic := "/meshsub/1.2.0", "topic-a"
	timestamp := time.Now().Add(-time.Second).UnixNano()
	for _, test := range []struct {
		event                         pubsubpb.TraceEvent
		wantType, wantPeer, wantTopic string
	}{
		{pubsubpb.TraceEvent{Type: pubsubpb.TraceEvent_ADD_PEER.Enum(), AddPeer: &pubsubpb.TraceEvent_AddPeer{PeerID: []byte(remote), Proto: &protocol}}, "add_peer", remote.String(), ""},
		{pubsubpb.TraceEvent{Type: pubsubpb.TraceEvent_REMOVE_PEER.Enum(), RemovePeer: &pubsubpb.TraceEvent_RemovePeer{PeerID: []byte(remote)}}, "remove_peer", remote.String(), ""},
		{pubsubpb.TraceEvent{Type: pubsubpb.TraceEvent_JOIN.Enum(), Join: &pubsubpb.TraceEvent_Join{Topic: &topic}}, "join", "", topic},
		{pubsubpb.TraceEvent{Type: pubsubpb.TraceEvent_LEAVE.Enum(), Leave: &pubsubpb.TraceEvent_Leave{Topic: &topic}}, "leave", "", topic},
	} {
		t.Run(test.wantType, func(t *testing.T) {
			telemetry := &telemetry{node: model.Node{ID: "node", RunID: "run"}, events: make(chan model.TraceEvent, 1)}
			tracer := &gossipTracer{telemetry: telemetry, peerID: "local"}
			test.event.Timestamp = &timestamp
			tracer.Trace(&test.event)
			select {
			case event := <-telemetry.events:
				if event.Type != test.wantType || event.RemotePeerID != test.wantPeer || event.Topic != test.wantTopic || event.Timestamp.UnixNano() != timestamp || event.RunID != "run" {
					t.Fatalf("lifecycle event=%+v", event)
				}
			default:
				t.Fatal("lifecycle event was dropped")
			}
		})
	}
}
