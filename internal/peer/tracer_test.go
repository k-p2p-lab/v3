package peer

import (
	"encoding/hex"
	"encoding/json"
	"testing"
	"time"

	"github.com/k-p2p-lab/v3/internal/model"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
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

func TestDuplicateTracerCorrelatesMessagesWithoutDoubleCounting(t *testing.T) {
	server, _ := publicationTestServer(t)
	remote := corepeer.ID("source-peer")
	now := time.Now().UTC()
	encoded, err := json.Marshal(envelope{ID: "application-message", RunID: "run", Publisher: "source-node", SentAt: now.Add(-25 * time.Millisecond).UnixNano(), Payload: []byte{42}})
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name, encoding, wantID string
		data                   []byte
	}{
		{"envelope", "envelope", "application-message", encoded},
		{"raw", "raw", "pubsub-" + hex.EncodeToString([]byte("native-wire-id")), []byte{42}},
	} {
		t.Run(test.name, func(t *testing.T) {
			// Discard startup peer/topic traces before checking exactly one copy.
			for len(server.telemetry.events) > 0 {
				<-server.telemetry.events
			}
			message := publicationTestMessage(test.data, "native-wire-id", remote)
			message.From = []byte(remote)
			tracer := &messageTracer{server: server}
			tracer.DuplicateMessage(message)
			(&gossipTracer{telemetry: server.telemetry, peerID: server.host.ID().String()}).Trace(&pubsubpb.TraceEvent{
				Type:             pubsubpb.TraceEvent_DUPLICATE_MESSAGE.Enum(),
				DuplicateMessage: &pubsubpb.TraceEvent_DuplicateMessage{MessageID: []byte(message.ID), Topic: message.Topic, ReceivedFrom: []byte(remote)},
			})
			if len(server.telemetry.events) != 1 {
				t.Fatalf("one router duplicate produced %d events", len(server.telemetry.events))
			}
			event := <-server.telemetry.events
			if event.Type != "duplicate" || event.MessageID != test.wantID || event.Fields["payloadEncoding"] != test.encoding || event.Fields["localDelivery"] != false || event.Fields["pubsubMessageId"] != hex.EncodeToString([]byte(message.ID)) || event.EventID == "" {
				t.Fatalf("duplicate metadata=%+v", event)
			}
			if _, exists := event.Fields["latencyAvailable"]; exists || event.LatencyMS != 0 {
				t.Fatal("router duplicate fabricated an application delivery latency")
			}
			if test.encoding == "envelope" && event.Fields["publisher"] != "source-node" {
				t.Fatalf("duplicate lost envelope publisher: %+v", event)
			}
			if test.encoding == "raw" {
				if _, exists := event.Fields["publisher"]; exists {
					t.Fatal("raw duplicate fabricated an application publisher")
				}
			}
			// Two actual wire copies remain distinct events for source-ID dedup.
			tracer.DuplicateMessage(message)
			second := <-server.telemetry.events
			if event.EventID == second.EventID || event.MessageID != second.MessageID {
				t.Fatalf("duplicate copies have incorrect identities: %+v %+v", event, second)
			}
		})
	}
	tracer := &messageTracer{server: server}
	tracer.DuplicateMessage(nil)
	tracer.DuplicateMessage(&pubsub.Message{})
}

func TestApplicationDeliverySeparatesRemoteLatencyFromLocalCopies(t *testing.T) {
	server, _ := publicationTestServer(t)
	sentAt := time.Now().UTC()
	data, err := json.Marshal(envelope{ID: "message", RunID: "run", Publisher: "source-node", SentAt: sentAt.UnixNano(), Payload: []byte{42}})
	if err != nil {
		t.Fatal(err)
	}
	message := publicationTestMessage(data, "native-wire-id", corepeer.ID("relay-peer"))
	message.From = []byte(corepeer.ID("source-peer"))
	receivedAt := sentAt.Add(42*time.Millisecond + 500*time.Microsecond)
	event, ok := server.deliveryEvent(message, "topic-a", receivedAt)
	if !ok || event.LatencyMS != 42.5 || event.Timestamp != receivedAt || event.Fields["localDelivery"] != false || event.Fields["latencyAvailable"] != true || event.Fields["publisher"] != "source-node" {
		t.Fatalf("remote application delivery=%+v", event)
	}
	message.ReceivedFrom = server.host.ID()
	event, ok = server.deliveryEvent(message, "topic-a", receivedAt)
	if !ok || event.Fields["localDelivery"] != true {
		t.Fatalf("local subscription delivery classified as remote: %+v", event)
	}
	message.ReceivedFrom = corepeer.ID("relay-peer")
	message.From = []byte(server.host.ID())
	message.Data = []byte{42}
	event, ok = server.deliveryEvent(message, "topic-a", receivedAt)
	if !ok || event.Fields["localDelivery"] != true || event.Fields["latencyAvailable"] != false {
		t.Fatalf("raw publisher-return copy classified as remote: %+v", event)
	}
	message.From = []byte(corepeer.ID("source-peer"))
	message.Data = data
	event, ok = server.deliveryEvent(message, "topic-a", sentAt.Add(-time.Millisecond))
	if !ok || event.LatencyMS >= 0 || event.Fields["clockSkewDetected"] != true {
		t.Fatalf("negative one-way latency was hidden: %+v", event)
	}
}
