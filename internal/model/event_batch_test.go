package model

import (
	"bytes"
	"encoding/json"
	"math"
	"strings"
	"testing"
)

func TestMarshalEventBatchPrefixUsesExactWireLimit(t *testing.T) {
	agentID := "agent<\"&"
	event := TraceEvent{EventID: "first", Fields: map[string]any{"padding": "", "counter": uint64(math.MaxUint64)}}
	empty, err := json.Marshal(EventBatch{AgentID: agentID, Events: []TraceEvent{event}})
	if err != nil {
		t.Fatal(err)
	}
	event.Fields["padding"] = strings.Repeat("x", MaxEventBatchBytes-len(empty))
	data, count, err := MarshalEventBatchPrefix(agentID, []TraceEvent{event, {EventID: "next"}})
	if err != nil || count != 1 || len(data) != MaxEventBatchBytes {
		t.Fatalf("boundary: count=%d bytes=%d err=%v", count, len(data), err)
	}
	want, err := json.Marshal(EventBatch{AgentID: agentID, Events: []TraceEvent{event}})
	if err != nil || !bytes.Equal(data, want) {
		t.Fatal("wire encoder changed envelope, escaping, or integer precision")
	}
	event.Fields["padding"] = event.Fields["padding"].(string) + "x"
	if _, count, err = MarshalEventBatchPrefix(agentID, []TraceEvent{event}); err == nil || count != 0 {
		t.Fatal("oversized single event accepted")
	}
}

func TestMarshalEventBatchPrefixRespectsCountAndDefersInvalidSuffix(t *testing.T) {
	events := make([]TraceEvent, MaxEventBatchEvents+1)
	data, count, err := MarshalEventBatchPrefix("agent", events)
	if err != nil || count != MaxEventBatchEvents {
		t.Fatalf("event count limit: count=%d err=%v", count, err)
	}
	var batch EventBatch
	if err := json.Unmarshal(data, &batch); err != nil || len(batch.Events) != count {
		t.Fatal("incorrect prefix encoding")
	}
	events = []TraceEvent{{EventID: "valid"}, {LatencyMS: math.NaN()}}
	data, count, err = MarshalEventBatchPrefix("agent", events)
	if err != nil || count != 1 || !json.Valid(data) {
		t.Fatalf("invalid suffix blocked valid prefix: count=%d err=%v", count, err)
	}
	if _, count, err = MarshalEventBatchPrefix("agent", events[1:]); err == nil || count != 0 {
		t.Fatal("invalid event was silently consumed")
	}
}
