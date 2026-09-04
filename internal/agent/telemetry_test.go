package agent

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/k-p2p-lab/v3/internal/model"
)

func TestTelemetryBackpressurePreservesAcknowledgedAndInFlightEvents(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	controller := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer controller.Close()
	s := &Server{config: Config{ID: "agent", ControllerURL: controller.URL}, client: controller.Client(), logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	s.events = make([]model.TraceEvent, 50000)
	s.events[0] = model.TraceEvent{EventID: "first", SessionID: "session", Sequence: 1}
	done := make(chan struct{})
	go func() { s.flushEvents(context.Background()); close(done) }()
	<-started
	request := httptest.NewRequest(http.MethodPost, "/api/v1/telemetry", strings.NewReader(`{"events":[{"eventId":"new","sessionId":"session","sequence":50001}]}`))
	response := httptest.NewRecorder()
	s.handleTelemetry(response, request)
	if response.Code != http.StatusServiceUnavailable || response.Header().Get("Retry-After") == "" {
		t.Fatalf("full queue acknowledged data: %d %s", response.Code, response.Body.String())
	}
	close(release)
	<-done
	if len(s.events) != 50000 || s.events[0].EventID != "first" || s.events[0].Sequence != 1 || s.eventsInFlight != 0 {
		t.Fatal("failed batch retry discarded or reordered acknowledged data")
	}
}

func TestTerminationRetainedAcrossFullQueueAndDrain(t *testing.T) {
	requests := 0
	controller := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.WriteHeader(http.StatusNoContent)
	}))
	defer controller.Close()
	s := &Server{config: Config{ID: "agent", ControllerURL: controller.URL}, client: controller.Client(), logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	s.events = make([]model.TraceEvent, 50000)
	event := model.TraceEvent{NodeID: "departed", Type: "measurement_terminated", Timestamp: time.Now()}
	s.queueTermination(event)
	s.eventsMu.Lock()
	s.admitTerminationsLocked()
	s.eventsMu.Unlock()
	if len(s.events) != 50000 || len(s.terminations) != 1 {
		t.Fatal("full queue lost termination marker")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.drainEvents(ctx); err != nil {
		t.Fatal(err)
	}
	if len(s.events) != 0 || len(s.terminations) != 0 || requests != 11 {
		t.Fatalf("incomplete multi-batch drain: %d queued, %d markers, %d requests", len(s.events), len(s.terminations), requests)
	}
}

func TestTelemetryDrainTimeoutIsAnErrorAndClosedAdmissionNeverAcknowledges(t *testing.T) {
	controller := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer controller.Close()
	s := &Server{config: Config{ID: "agent", ControllerURL: controller.URL}, client: controller.Client(), logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	s.enqueueEvents(model.EventBatch{Events: []model.TraceEvent{{EventID: "accepted"}}})
	s.closeTelemetry()
	response := httptest.NewRecorder()
	s.handleTelemetry(response, httptest.NewRequest(http.MethodPost, "/api/v1/telemetry", strings.NewReader(`{"events":[{"eventId":"after-close"}]}`)))
	if response.Code != http.StatusServiceUnavailable || len(s.events) != 1 {
		t.Fatal("request was acknowledged beyond the drain boundary")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := s.drainEvents(ctx); !errors.Is(err, context.DeadlineExceeded) || len(s.events) != 1 {
		t.Fatalf("drain falsely reported success or discarded retry data: %v", err)
	}
}
