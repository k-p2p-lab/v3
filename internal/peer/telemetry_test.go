package peer

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/k-p2p-lab/v3/internal/model"
)

func TestTelemetryEventIdentitySurvivesQueueAndResend(t *testing.T) {
	received := make(chan model.TraceEvent, 2)
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var batch model.EventBatch
		if err := json.NewDecoder(r.Body).Decode(&batch); err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		for _, event := range batch.Events {
			received <- event
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer endpoint.Close()
	tel := newTelemetry(model.Node{ID: "node", RunID: "run"}, endpoint.URL, "", slog.New(slog.NewTextHandler(io.Discard, nil)))
	input := model.TraceEvent{Type: "duplicate", MessageID: "message", Timestamp: time.Now().UTC()}
	tel.emit(input)
	first := <-tel.events
	if first.EventID == "" || first.NodeID != "node" || first.RunID != "run" {
		t.Fatalf("source event has no identity: %+v", first)
	}
	tel.emit(first)
	retry := <-tel.events
	if !reflect.DeepEqual(first, retry) {
		t.Fatalf("requeue changed event: %+v %+v", first, retry)
	}
	tel.flush(context.Background(), []model.TraceEvent{first})
	tel.flush(context.Background(), []model.TraceEvent{retry})
	if firstWire, retryWire := <-received, <-received; !reflect.DeepEqual(firstWire, retryWire) || firstWire.EventID != first.EventID {
		t.Fatalf("resend changed event identity: %+v %+v", firstWire, retryWire)
	}
	tel.emit(input)
	if next := <-tel.events; next.EventID == first.EventID {
		t.Fatal("two source observations were given the same event ID")
	}
	// Drop reports bypass the bounded queue but use the same source identity path.
	drop := tel.identify(model.TraceEvent{Type: "telemetry_drop", Fields: map[string]any{"count": uint64(7)}})
	if drop.EventID == "" || drop.EventID == first.EventID || drop.Timestamp.IsZero() || drop.RunID != "run" || drop.NodeID != "node" {
		t.Fatalf("drop report lacks source identity: %+v", drop)
	}
}
