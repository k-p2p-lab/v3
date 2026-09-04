package peer

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/k-p2p-lab/v3/internal/model"
)

type telemetry struct {
	node               model.Node
	agentURL           string
	token              string
	client             *http.Client
	logger             *slog.Logger
	events             chan model.TraceEvent
	dropped            atomic.Uint64
	mu                 sync.Mutex // Source sequence, receipt timestamps, and queue admission.
	sessionID          string
	sequence           uint64
	observedAt         time.Time
	measurementStarted bool
	measurementStopped bool
	measurementDone    chan struct{}
	sealed             bool
	shutdownTimeout    time.Duration
	retryInterval      time.Duration
}

const telemetryShutdownTimeout = 20 * time.Second

func newTelemetry(node model.Node, agentURL, token string, logger *slog.Logger) *telemetry {
	if logger == nil {
		logger = slog.Default()
	}
	return &telemetry{
		node:     node,
		agentURL: strings.TrimRight(agentURL, "/"),
		token:    token,
		client:   &http.Client{Timeout: 5 * time.Second},
		logger:   logger,
		events:   make(chan model.TraceEvent, 10000),
	}
}

func (t *telemetry) emit(event model.TraceEvent) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.sealed {
		t.reportDropsLocked(false)
	}
	t.enqueueLocked(event, false)
}

// Receipt timestamps are created inside the same critical section as a
// checkpoint. A checkpoint therefore never certifies a delivery whose earlier
// receipt timestamp has been assigned but whose event is still awaiting enqueue.
func (t *telemetry) emitObserved(build func(time.Time) (model.TraceEvent, bool)) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if event, ok := build(t.observationTimeLocked()); ok {
		if !t.sealed {
			t.reportDropsLocked(false)
		}
		t.enqueueLocked(event, false)
	}
}

func (t *telemetry) observationTimeLocked() time.Time {
	now := time.Now().UTC()
	if !now.After(t.observedAt) {
		now = t.observedAt.Add(time.Nanosecond)
	}
	t.observedAt = now
	return now
}

// Leave two slots for the final drop notice and stop checkpoint when the
// ordinary queue is saturated. Small queues used by embedders/tests still work.
func (t *telemetry) queueLimit(priority bool) int {
	limit := cap(t.events)
	if !priority && limit >= 4 {
		limit -= 2
	}
	return limit
}

func (t *telemetry) enqueueLocked(event model.TraceEvent, priority bool) {
	event = t.identifyLocked(event)
	if t.sealed || (cap(t.events) > 0 && len(t.events) >= t.queueLimit(priority)) {
		t.dropped.Add(1)
		return
	}
	select {
	case t.events <- event:
	default:
		t.dropped.Add(1)
	}
}

func (t *telemetry) identifyLocked(event model.TraceEvent) model.TraceEvent {
	if t.sessionID == "" {
		t.sessionID = rand.Text()
	}
	t.sequence++
	event.SessionID = t.sessionID
	event.Sequence = t.sequence
	event.RunID = t.node.RunID
	event.NodeID = t.node.ID
	if event.EventID == "" {
		// Assigned at the source, so Agent queue retries retain the same ID.
		event.EventID = rand.Text()
	}
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now().UTC()
	}
	return event
}

func (t *telemetry) reportDropsLocked(priority bool) {
	if t.dropped.Load() == 0 || len(t.events) >= t.queueLimit(priority) {
		return
	}
	count := t.dropped.Swap(0)
	event := t.identifyLocked(model.TraceEvent{Type: "telemetry_drop", Fields: map[string]any{"count": count}})
	// All producers hold mu, and the consumer can only free capacity.
	t.events <- event
}

func (t *telemetry) startMeasurement(topics []string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.measurementStarted || t.measurementStopped {
		return false
	}
	t.measurementStarted = true
	t.measurementDone = make(chan struct{})
	t.reportDropsLocked(false)
	t.enqueueLocked(model.TraceEvent{Type: "measurement_start", Timestamp: t.observationTimeLocked(), Fields: map[string]any{"subscribedTopics": append([]string{}, topics...)}}, false)
	return true
}

func (t *telemetry) checkpoint() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.measurementStarted && !t.measurementStopped {
		t.reportDropsLocked(false)
		t.enqueueLocked(model.TraceEvent{Type: "measurement_checkpoint", Timestamp: t.observationTimeLocked()}, false)
	}
}

func (t *telemetry) stopMeasurement() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.measurementStopped {
		return
	}
	t.measurementStopped = true
	if t.measurementStarted {
		t.reportDropsLocked(true)
		t.enqueueLocked(model.TraceEvent{Type: "measurement_stop", Timestamp: t.observationTimeLocked()}, true)
		close(t.measurementDone)
	}
}

func (t *telemetry) measurementLoop(ctx context.Context) {
	t.mu.Lock()
	done := t.measurementDone
	t.mu.Unlock()
	if done == nil {
		return
	}
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			return
		case <-ctx.Done():
			t.stopMeasurement()
			return
		case <-ticker.C:
			t.checkpoint()
		}
	}
}

func (t *telemetry) run(ctx context.Context) error {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	timeout := t.shutdownTimeout
	if timeout <= 0 {
		timeout = telemetryShutdownTimeout
	}
	retry := t.retryInterval
	if retry <= 0 {
		retry = 250 * time.Millisecond
	}
	var drainCtx context.Context
	var drainCancel context.CancelFunc
	defer func() {
		if drainCancel != nil {
			drainCancel()
		}
	}()
	batch := make([]model.TraceEvent, 0, 250)
	flush := false
	for {
		if ctx.Err() != nil && drainCtx == nil {
			drainCtx, drainCancel = context.WithTimeout(context.Background(), timeout)
			t.mu.Lock()
			t.sealed = true
			t.mu.Unlock()
		}
		activeCtx := ctx
		if drainCtx != nil {
			activeCtx = drainCtx
			if err := drainCtx.Err(); err != nil {
				return fmt.Errorf("drain peer telemetry (%d queued, %d pending): %w", len(t.events), len(batch), err)
			}
		}
		if len(batch) > 0 && (flush || len(batch) == cap(batch) || drainCtx != nil) {
			if err := t.flush(activeCtx, batch); err != nil {
				if t.logger != nil {
					t.logger.Warn("retry peer telemetry", "events", len(batch), "error", err)
				}
				wait := time.NewTimer(retry)
				select {
				case <-activeCtx.Done():
				case <-wait.C:
				}
				wait.Stop()
				flush = true
				continue // Keep this bounded batch, preserving source order and IDs.
			}
			batch = batch[:0]
			flush = false
		}
		t.mu.Lock()
		t.reportDropsLocked(drainCtx != nil)
		t.mu.Unlock()
		if drainCtx != nil {
			for len(batch) < cap(batch) {
				select {
				case event := <-t.events:
					batch = append(batch, event)
				default:
					if len(batch) == 0 {
						return nil
					}
					flush = true
				}
				if flush {
					break
				}
			}
			continue
		}
		select {
		case <-ctx.Done():
		case event := <-t.events:
			batch = append(batch, event)
		case <-ticker.C:
			flush = true
		}
	}
}

func (t *telemetry) flush(ctx context.Context, events []model.TraceEvent) error {
	if len(events) == 0 {
		return nil
	}
	batch := model.EventBatch{Events: events}
	data, err := json.Marshal(batch)
	if err != nil {
		return fmt.Errorf("encode telemetry: %w", err)
	}
	flushCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(flushCtx, http.MethodPost, t.agentURL+"/api/v1/telemetry", bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if t.token != "" {
		req.Header.Set("Authorization", "Bearer "+t.token)
	}
	resp, err := t.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		message, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("agent returned %s: %s", resp.Status, strings.TrimSpace(string(message)))
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 2048))
	return nil
}

func (t *telemetry) reportNode(ctx context.Context, node model.Node) error {
	data, err := json.Marshal(node)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.agentURL+"/api/v1/nodes/"+node.ID+"/status", bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if t.token != "" {
		req.Header.Set("Authorization", "Bearer "+t.token)
	}
	resp, err := t.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		message, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("agent returned %s: %s", resp.Status, strings.TrimSpace(string(message)))
	}
	return nil
}
