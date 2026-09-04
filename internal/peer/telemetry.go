package peer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/k-p2p-lab/v3/internal/model"
)

type telemetry struct {
	node     model.Node
	agentURL string
	token    string
	client   *http.Client
	logger   *slog.Logger
	events   chan model.TraceEvent
	dropped  atomic.Uint64
}

func newTelemetry(node model.Node, agentURL, token string, logger *slog.Logger) *telemetry {
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
	event.RunID = t.node.RunID
	event.NodeID = t.node.ID
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now().UTC()
	}
	select {
	case t.events <- event:
	default:
		t.dropped.Add(1)
	}
}

func (t *telemetry) run(ctx context.Context) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	batch := make([]model.TraceEvent, 0, 250)
	for {
		select {
		case <-ctx.Done():
			t.flush(context.Background(), batch)
			return
		case event := <-t.events:
			batch = append(batch, event)
			if len(batch) >= 250 {
				t.flush(ctx, batch)
				batch = make([]model.TraceEvent, 0, 250)
			}
		case <-ticker.C:
			dropped := t.dropped.Swap(0)
			if dropped > 0 {
				batch = append(batch, model.TraceEvent{Type: "telemetry_drop", Timestamp: time.Now().UTC(), Fields: map[string]any{"count": dropped}})
			}
			if len(batch) > 0 {
				t.flush(ctx, batch)
				batch = make([]model.TraceEvent, 0, 250)
			}
		}
	}
}

func (t *telemetry) flush(ctx context.Context, events []model.TraceEvent) {
	if len(events) == 0 {
		return
	}
	batch := model.EventBatch{Events: events}
	data, err := json.Marshal(batch)
	if err != nil {
		t.logger.Error("encode telemetry", "error", err)
		return
	}
	flushCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(flushCtx, http.MethodPost, t.agentURL+"/api/v1/telemetry", bytes.NewReader(data))
	if err != nil {
		t.logger.Warn("create telemetry request", "error", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	if t.token != "" {
		req.Header.Set("Authorization", "Bearer "+t.token)
	}
	resp, err := t.client.Do(req)
	if err != nil {
		t.logger.Warn("send telemetry", "events", len(events), "error", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		message, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		t.logger.Warn("telemetry rejected", "status", resp.Status, "message", strings.TrimSpace(string(message)))
	}
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
