package peer

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/k-p2p-lab/v3/internal/model"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
)

func publicationTestServer(t *testing.T) (*Server, context.Context) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	node := model.Node{ID: "node", RunID: "run"}
	config := model.NodeConfig{GossipSub: model.GossipSubConfig{Topics: []string{"topic-b", "topic-a"}}}.WithDefaults()
	server := &Server{config: model.PeerProcessConfig{Node: node, NodeConfig: config}, host: newConfigTestHost(t), topics: make(map[string]*pubsub.Topic), telemetry: &telemetry{node: node, events: make(chan model.TraceEvent, 100)}}
	if err := server.startPubSub(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(server.Close)
	return server, ctx
}

func TestRawPublicationHasExactDataLengthAndUnavailableLatency(t *testing.T) {
	server, _ := publicationTestServer(t)
	for _, size := range []int{1, 32, 1024, 4096} {
		message, err := server.preparePublication(model.PublishRequest{PayloadSize: size, PayloadEncoding: "raw"}, time.Now().UTC())
		if err != nil {
			t.Fatal(err)
		}
		if len(message.wire) != size || message.encoding != "raw" {
			t.Fatalf("requested=%d data=%d encoding=%s", size, len(message.wire), message.encoding)
		}
		event, ok := server.deliveryEvent(message.wire, "topic-a", "previous-hop", time.Now().UTC())
		if !ok || event.MessageID != message.id || event.LatencyMS != -1 || event.Fields["latencyAvailable"] != false || event.RemotePeerID != "previous-hop" {
			t.Fatalf("raw delivery metadata=%+v", event)
		}
		if _, exists := event.Fields["publisher"]; exists {
			t.Fatal("raw delivery fabricated publisher metadata")
		}
		if event.Fields["payloadBytes"] != size || event.Fields["wireBytes"] != size {
			t.Fatalf("incorrect byte counts: %+v", event.Fields)
		}
	}
}

func TestDefaultEnvelopeRetainsCorrelationAndLatency(t *testing.T) {
	server, _ := publicationTestServer(t)
	now := time.Now().UTC()
	message, err := server.preparePublication(model.PublishRequest{PayloadSize: 32}, now)
	if err != nil {
		t.Fatal(err)
	}
	if message.encoding != "envelope" || len(message.wire) <= 32 {
		t.Fatalf("default encoding changed: %+v", message)
	}
	event, ok := server.deliveryEvent(message.wire, "topic-a", "previous-hop", now.Add(40*time.Millisecond))
	if !ok || event.MessageID != message.id || event.LatencyMS != 40 || event.Fields["publisher"] != "node" || event.Fields["payloadBytes"] != 32 {
		t.Fatalf("envelope metadata=%+v", event)
	}
	var envelope envelope
	if err := json.Unmarshal(message.wire, &envelope); err != nil {
		t.Fatal(err)
	}
	envelope.RunID = "other-run"
	foreign, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := server.deliveryEvent(foreign, "topic-a", "previous-hop", now); ok {
		t.Fatal("foreign envelope passed run isolation")
	}
}

func TestAllTopicsRawPublishUsesExactBytesAndDistinctMessages(t *testing.T) {
	server, parent := publicationTestServer(t)
	subs := make(map[string]*pubsub.Subscription)
	for topicName, topic := range server.topics {
		sub, err := topic.Subscribe()
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(sub.Cancel)
		subs[topicName] = sub
	}
	request := httptest.NewRequest(http.MethodPost, "/publish", strings.NewReader(`{"topic":"*","payloadSize":1024,"payloadEncoding":"raw","targetNodes":99,"targetNodesByTopic":{"topic-a":4,"topic-b":7}}`))
	response := httptest.NewRecorder()
	server.handler().ServeHTTP(response, request)
	if response.Code != http.StatusAccepted {
		t.Fatalf("publish status=%d body=%s", response.Code, response.Body.String())
	}
	var result struct {
		Messages []struct {
			Topic     string `json:"topic"`
			MessageID string `json:"messageId"`
			WireBytes int    `json:"wireBytes"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Messages) != 2 || result.Messages[0].Topic != "topic-a" || result.Messages[1].Topic != "topic-b" {
		t.Fatalf("all-topic response=%s", response.Body.String())
	}
	ctx, cancel := context.WithTimeout(parent, 2*time.Second)
	defer cancel()
	var previous []byte
	for _, expected := range result.Messages {
		message, err := subs[expected.Topic].Next(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(message.Data) != 1024 || expected.WireBytes != 1024 || rawMessageID(message.Data) != expected.MessageID {
			t.Fatalf("incorrect raw PubSub data for %s: bytes=%d result=%+v", expected.Topic, len(message.Data), expected)
		}
		if previous != nil && bytes.Equal(previous, message.Data) {
			t.Fatal("all-topic publication reused the same random payload")
		}
		previous = message.Data
	}
	counts := make(map[string]int)
	for len(server.telemetry.events) > 0 {
		event := <-server.telemetry.events
		if event.Type == "publish" {
			counts[event.Topic], _ = event.Fields["targetNodes"].(int)
		}
	}
	if !reflect.DeepEqual(counts, map[string]int{"topic-a": 4, "topic-b": 7}) {
		t.Fatalf("wildcard publication lost per-topic reachability targets: %v", counts)
	}
}

func TestPublishTopicsAndEncodingValidation(t *testing.T) {
	server, _ := publicationTestServer(t)
	for _, test := range []struct {
		requested string
		want      []string
	}{
		{"", []string{"topic-b"}},
		{"topic-a", []string{"topic-a"}},
		{"*", []string{"topic-a", "topic-b"}},
	} {
		topics, err := server.publishTopics(test.requested)
		if err != nil || !reflect.DeepEqual(topics, test.want) {
			t.Fatalf("publishTopics(%q)=%v, %v", test.requested, topics, err)
		}
	}
	for _, data := range []string{`{"topic":"missing","payloadSize":32}`, `{"topic":"*","payloadEncoding":"invalid","payloadSize":32}`} {
		response := httptest.NewRecorder()
		server.handler().ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/publish", strings.NewReader(data)))
		if response.Code != http.StatusBadRequest {
			t.Fatalf("invalid publish accepted: %d %s", response.Code, response.Body.String())
		}
	}
}
