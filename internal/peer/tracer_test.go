package peer

import (
	"encoding/hex"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/k-p2p-lab/v3/internal/model"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	pubsubpb "github.com/libp2p/go-libp2p-pubsub/pb"
	corepeer "github.com/libp2p/go-libp2p/core/peer"
)

func TestGossipTracerAggregatesEveryControlTypeInMixedRPC(t *testing.T) {
	nodes := bootstrapTestNodes(t, 1)
	remote, err := corepeer.Decode(nodes[0].PeerID)
	if err != nil {
		t.Fatal(err)
	}
	topicA, topicB := "topic-a", "topic-b"
	timestamp := time.Now().Add(-time.Second).UnixNano()
	telemetry := &telemetry{node: model.Node{ID: "node", RunID: "run"}, events: make(chan model.TraceEvent, 10)}
	tracer := &gossipTracer{telemetry: telemetry, peerID: "local"}
	tracer.Trace(&pubsubpb.TraceEvent{
		Type: pubsubpb.TraceEvent_SEND_RPC.Enum(), Timestamp: &timestamp,
		SendRPC: &pubsubpb.TraceEvent_SendRPC{SendTo: []byte(remote), Meta: &pubsubpb.TraceEvent_RPCMeta{Control: &pubsubpb.TraceEvent_ControlMeta{
			Ihave: []*pubsubpb.TraceEvent_ControlIHaveMeta{
				{Topic: &topicB, MessageIDs: [][]byte{[]byte("one"), []byte("two")}},
				{Topic: &topicA, MessageIDs: [][]byte{[]byte("three")}},
			},
			Iwant:     []*pubsubpb.TraceEvent_ControlIWantMeta{{MessageIDs: [][]byte{[]byte("one"), []byte("three")}}},
			Idontwant: []*pubsubpb.TraceEvent_ControlIDontWantMeta{{MessageIDs: [][]byte{[]byte("two")}}},
			Graft:     []*pubsubpb.TraceEvent_ControlGraftMeta{{Topic: &topicA}, {Topic: &topicB}},
			Prune:     []*pubsubpb.TraceEvent_ControlPruneMeta{{Topic: &topicA, Peers: [][]byte{[]byte("px-1"), []byte("px-2")}}},
		}}},
	})

	events := make(map[string]model.TraceEvent)
	for range 5 {
		select {
		case event := <-telemetry.events:
			events[event.Type] = event
		default:
			t.Fatalf("only %d mixed control events were emitted", len(events))
		}
	}
	if len(events) != 5 {
		t.Fatalf("mixed RPC events = %v", events)
	}
	for eventType, event := range events {
		if event.RemotePeerID != remote.String() || event.Timestamp.UnixNano() != timestamp || event.RunID != "run" || event.EventID == "" {
			t.Fatalf("%s metadata=%+v", eventType, event)
		}
	}
	ihave := events["send_ihave"]
	if ihave.Topic != "" || ihave.Fields["controlEntries"] != 2 || ihave.Fields["messageIdCount"] != 3 || !reflect.DeepEqual(ihave.Fields["topics"], []string{topicA, topicB}) ||
		!reflect.DeepEqual(ihave.Fields["topicEntryCounts"], map[string]int{topicA: 1, topicB: 1}) || !reflect.DeepEqual(ihave.Fields["topicMessageIdCounts"], map[string]int{topicA: 1, topicB: 2}) {
		t.Fatalf("IHAVE aggregation=%+v", ihave)
	}
	if event := events["send_iwant"]; event.Fields["controlEntries"] != 1 || event.Fields["messageIdCount"] != 2 {
		t.Fatalf("IWANT aggregation=%+v", event)
	}
	if event := events["send_idontwant"]; event.Fields["controlEntries"] != 1 || event.Fields["messageIdCount"] != 1 {
		t.Fatalf("IDONTWANT aggregation=%+v", event)
	}
	if event := events["send_graft"]; event.Fields["controlEntries"] != 2 || event.Fields["messageIdCount"] != 0 {
		t.Fatalf("GRAFT aggregation=%+v", event)
	}
	if event := events["send_prune"]; event.Topic != "" || event.Fields["controlEntries"] != 1 || event.Fields["peerExchangeCount"] != 2 {
		t.Fatalf("PRUNE aggregation=%+v", event)
	}
}

func TestGossipTracerSeparatesReceivedAndDroppedControlRPCs(t *testing.T) {
	nodes := bootstrapTestNodes(t, 1)
	remote, err := corepeer.Decode(nodes[0].PeerID)
	if err != nil {
		t.Fatal(err)
	}
	topic := "topic"
	meta := &pubsubpb.TraceEvent_RPCMeta{Control: &pubsubpb.TraceEvent_ControlMeta{
		Ihave: []*pubsubpb.TraceEvent_ControlIHaveMeta{{Topic: &topic, MessageIDs: [][]byte{[]byte("one")}}},
	}}
	for _, test := range []struct {
		name, want string
		event      pubsubpb.TraceEvent
	}{
		{"receive", "recv_ihave", pubsubpb.TraceEvent{Type: pubsubpb.TraceEvent_RECV_RPC.Enum(), RecvRPC: &pubsubpb.TraceEvent_RecvRPC{ReceivedFrom: []byte(remote), Meta: meta}}},
		{"drop", "drop_ihave", pubsubpb.TraceEvent{Type: pubsubpb.TraceEvent_DROP_RPC.Enum(), DropRPC: &pubsubpb.TraceEvent_DropRPC{SendTo: []byte(remote), Meta: meta}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			telemetry := &telemetry{node: model.Node{RunID: "run"}, events: make(chan model.TraceEvent, 1)}
			(&gossipTracer{telemetry: telemetry}).Trace(&test.event)
			event := <-telemetry.events
			if event.Type != test.want || event.Topic != "" || event.Fields["rpcCount"] != 1 || event.Fields["direction"] != test.want[:len(test.want)-len("_ihave")] || !reflect.DeepEqual(event.Fields["topics"], []string{topic}) {
				t.Fatalf("event=%+v", event)
			}
		})
	}
	telemetry := &telemetry{events: make(chan model.TraceEvent, 1)}
	(&gossipTracer{telemetry: telemetry}).Trace(&pubsubpb.TraceEvent{Type: pubsubpb.TraceEvent_SEND_RPC.Enum(), SendRPC: &pubsubpb.TraceEvent_SendRPC{}})
	if len(telemetry.events) != 0 {
		t.Fatal("an RPC without control metadata produced telemetry")
	}
}

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
