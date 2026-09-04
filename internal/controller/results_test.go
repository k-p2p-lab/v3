package controller

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/k-p2p-lab/v3/internal/model"
)

func resultFixture(t *testing.T, server *Server, id, state string, started time.Time) (model.Experiment, []byte) {
	t.Helper()
	experiment := model.Experiment{ID: id, Name: "Saved " + id, State: state, StartedAt: started}
	if state != "running" {
		experiment.FinishedAt = started.Add(time.Minute)
	}
	if err := server.persistManifest(experiment, []byte("version: 3\nname: original scenario\n")); err != nil {
		t.Fatal(err)
	}
	metadata, err := os.ReadFile(filepath.Join(server.config.DataDir, "runs", id, "experiment.json"))
	if err != nil {
		t.Fatal(err)
	}
	return experiment, metadata
}

func resultRequest(server *Server, method, path string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	server.Handler(context.Background()).ServeHTTP(recorder, httptest.NewRequest(method, path, nil))
	return recorder
}

func decodeResultZIP(t *testing.T, data []byte) map[string][]byte {
	t.Helper()
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	files := make(map[string][]byte)
	for _, file := range reader.File {
		if _, duplicate := files[file.Name]; duplicate {
			t.Fatalf("duplicate ZIP entry %q", file.Name)
		}
		opened, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		contents, err := io.ReadAll(opened)
		_ = opened.Close()
		if err != nil {
			t.Fatal(err)
		}
		files[file.Name] = contents
	}
	return files
}

func TestResultsListReadsDiskAfterRestartAndReportsUnreadableMetadata(t *testing.T) {
	var logs bytes.Buffer
	server := New(ServerConfig{DataDir: t.TempDir(), Token: "secret"}, slog.New(slog.NewTextHandler(&logs, nil)))
	started := time.Date(2026, 9, 4, 1, 0, 0, 0, time.UTC)
	_, original := resultFixture(t, server, "run-old", "running", started)
	resultFixture(t, server, "run-new", "completed", started.Add(time.Hour))
	resultFixture(t, server, "run-corrupt", "completed", started)
	if err := os.WriteFile(filepath.Join(server.config.DataDir, "runs", "run-corrupt", "experiment.json"), []byte("broken json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(server.config.DataDir, "runs", "run-missing"), 0o755); err != nil {
		t.Fatal(err)
	}
	// A new Controller has no active runs, even though old metadata says running.
	restarted := New(server.config, server.logger)
	response := resultRequest(restarted, http.MethodGet, "/api/v1/results")
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body)
	}
	var results []savedResult
	if err := json.Unmarshal(response.Body.Bytes(), &results); err != nil {
		t.Fatal(err)
	}
	if len(results) != 4 || results[0].ID != "run-new" || results[1].ID != "run-old" || results[1].State != "interrupted" || results[1].Active {
		t.Fatalf("unexpected results: %+v", results)
	}
	for _, result := range results[2:] {
		if result.State != "unreadable" || result.Active || result.Name != result.ID {
			t.Fatalf("corrupt result was hidden or misrepresented: %+v", result)
		}
	}
	if !strings.Contains(logs.String(), "run-corrupt") || !strings.Contains(logs.String(), "run-missing") {
		t.Fatalf("missing warnings: %s", logs.String())
	}
	unchanged, _ := os.ReadFile(filepath.Join(server.config.DataDir, "runs", "run-old", "experiment.json"))
	if !bytes.Equal(original, unchanged) {
		t.Fatal("listing changed stored metadata")
	}
}

func TestResultDownloadIncludesFullPersistedFilesAndOriginalInterruptedState(t *testing.T) {
	server := New(ServerConfig{DataDir: t.TempDir(), Token: "secret"}, nil)
	_, original := resultFixture(t, server, "run-export", "running", time.Now().UTC())
	dir := filepath.Join(server.config.DataDir, "runs", "run-export")
	events := []byte(strings.Repeat("{\"kind\":\"deliver\"}\n", recentEventLimit+200))
	if err := os.WriteFile(filepath.Join(dir, "events.jsonl"), events, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "agent-secret.json"), []byte("must not export"), 0o600); err != nil {
		t.Fatal(err)
	}
	response := resultRequest(server, http.MethodGet, "/api/v1/experiments/run-export/download")
	if response.Code != http.StatusOK || response.Header().Get("Content-Type") != "application/zip" || response.Header().Get("Content-Disposition") != `attachment; filename="run-export.zip"` {
		t.Fatalf("download status=%d headers=%v body=%s", response.Code, response.Header(), response.Body)
	}
	files := decodeResultZIP(t, response.Body.Bytes())
	if len(files) != 5 || !json.Valid(files["metrics.json"]) || !bytes.Equal(files["experiment.json"], original) || !bytes.Equal(files["events.jsonl"], events) || string(files["scenario.yaml"]) != "version: 3\nname: original scenario\n" {
		t.Fatalf("incorrect archive contents: %v", files)
	}
	var exported struct {
		State, StoredState string
		Partial, Active    bool
		EventBytes         int64
		Files              map[string]int64
		ExportedAt         time.Time
	}
	if err := json.Unmarshal(files["export.json"], &exported); err != nil {
		t.Fatal(err)
	}
	if exported.State != "interrupted" || exported.StoredState != "running" || !exported.Partial || exported.Active || exported.EventBytes != int64(len(events)) || exported.ExportedAt.IsZero() || exported.Files["events.jsonl"] != int64(len(events)) {
		t.Fatalf("incorrect export metadata: %+v", exported)
	}
}

func TestResultSnapshotPinsFileDescriptorsAndEventLengths(t *testing.T) {
	server := New(ServerConfig{DataDir: t.TempDir()}, nil)
	experiment, original := resultFixture(t, server, "run-active", "running", time.Now().UTC())
	server.state.experiments[experiment.ID] = experiment
	listed := resultRequest(server, http.MethodGet, "/api/v1/results")
	var activeResults []savedResult
	if err := json.Unmarshal(listed.Body.Bytes(), &activeResults); err != nil {
		t.Fatal(err)
	}
	if len(activeResults) != 1 || !activeResults[0].Active || activeResults[0].State != "running" {
		t.Fatalf("current run not marked active: %+v", activeResults)
	}
	first := model.EventBatch{Events: []model.TraceEvent{{RunID: experiment.ID, Type: "before"}}}
	if err := server.state.appendEvents(first); err != nil {
		t.Fatal(err)
	}
	eventPath := filepath.Join(server.config.DataDir, "runs", experiment.ID, "events.jsonl")
	originalEvents, err := os.ReadFile(eventPath)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := server.captureResult(experiment.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer snapshot.close()
	if err := server.state.appendEvents(model.EventBatch{Events: []model.TraceEvent{{RunID: experiment.ID, Type: "after"}}}); err != nil {
		t.Fatal(err)
	}
	server.updateExperiment(experiment.ID, func(run *model.Experiment) { run.State = "completed" })
	var output bytes.Buffer
	if err := snapshot.writeZIP(context.Background(), &output); err != nil {
		t.Fatal(err)
	}
	files := decodeResultZIP(t, output.Bytes())
	if !bytes.Equal(files["experiment.json"], original) || !bytes.Equal(files["events.jsonl"], originalEvents) {
		t.Fatal("snapshot included replaced metadata or later events")
	}
	if !snapshot.result.Active || snapshot.result.State != "running" {
		t.Fatalf("active state was not captured: %+v", snapshot.result)
	}
}

type blockedResultWriter struct {
	bytes.Buffer
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (writer *blockedResultWriter) Write(data []byte) (int, error) {
	writer.once.Do(func() { close(writer.started); <-writer.release })
	return writer.Buffer.Write(data)
}

func TestResultStreamingDoesNotBlockPersistence(t *testing.T) {
	server := New(ServerConfig{DataDir: t.TempDir()}, nil)
	experiment, _ := resultFixture(t, server, "run-stream", "running", time.Now().UTC())
	server.state.experiments[experiment.ID] = experiment
	snapshot, err := server.captureResult(experiment.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer snapshot.close()
	writer := &blockedResultWriter{started: make(chan struct{}), release: make(chan struct{})}
	streamed := make(chan error, 1)
	go func() { streamed <- snapshot.writeZIP(context.Background(), writer) }()
	select {
	case <-writer.started:
	case <-time.After(5 * time.Second):
		t.Fatal("ZIP did not start streaming")
	}
	persisted := make(chan error, 1)
	go func() {
		err := server.state.appendEvents(model.EventBatch{Events: []model.TraceEvent{{RunID: experiment.ID, Type: "while-downloading"}}})
		server.updateExperiment(experiment.ID, func(run *model.Experiment) { run.State = "completed" })
		persisted <- err
	}()
	select {
	case err := <-persisted:
		if err != nil {
			t.Error(err)
		}
	case <-time.After(5 * time.Second):
		t.Error("slow download blocked event or metadata persistence")
	}
	close(writer.release)
	if err := <-streamed; err != nil {
		t.Fatal(err)
	}
	files := decodeResultZIP(t, writer.Bytes())
	if data, exists := files["events.jsonl"]; !exists || len(data) != 0 {
		t.Fatal("snapshot before the first event must contain empty events.jsonl")
	}
}

func TestResultDownloadRejectsUnreadableFilesBeforeZIPHeaders(t *testing.T) {
	for _, failure := range []string{"missing-metadata", "malformed-metadata", "mismatched-id", "oversized-metadata", "missing-scenario", "directory-events"} {
		t.Run(failure, func(t *testing.T) {
			server := New(ServerConfig{DataDir: t.TempDir()}, slog.New(slog.NewTextHandler(io.Discard, nil)))
			resultFixture(t, server, "run-test", "completed", time.Now().UTC())
			dir := filepath.Join(server.config.DataDir, "runs", "run-test")
			metadata := filepath.Join(dir, "experiment.json")
			var err error
			switch failure {
			case "missing-metadata":
				err = os.Remove(metadata)
			case "malformed-metadata":
				err = os.WriteFile(metadata, []byte("not JSON"), 0o644)
			case "mismatched-id":
				err = os.WriteFile(metadata, []byte(`{"id":"another-run","state":"completed"}`), 0o644)
			case "oversized-metadata":
				err = os.WriteFile(metadata, bytes.Repeat([]byte(" "), resultMetadataLimit+1), 0o644)
			case "missing-scenario":
				err = os.Remove(filepath.Join(dir, "scenario.yaml"))
			case "directory-events":
				err = os.Mkdir(filepath.Join(dir, "events.jsonl"), 0o755)
			}
			if err != nil {
				t.Fatal(err)
			}
			response := resultRequest(server, http.MethodGet, "/api/v1/experiments/run-test/download")
			if response.Code != http.StatusInternalServerError || response.Header().Get("Content-Disposition") != "" || !strings.HasPrefix(response.Header().Get("Content-Type"), "application/json") {
				t.Fatalf("unreadable result response=%d %v %s", response.Code, response.Header(), response.Body)
			}
		})
	}
}

func TestResultSnapshotTruncationCannotProduceSuccessfulZIP(t *testing.T) {
	server := New(ServerConfig{DataDir: t.TempDir()}, nil)
	resultFixture(t, server, "run-truncated", "completed", time.Now().UTC())
	path := filepath.Join(server.config.DataDir, "runs", "run-truncated", "events.jsonl")
	if err := os.WriteFile(path, []byte("{\"kind\":\"deliver\"}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	snapshot, err := server.captureResult("run-truncated")
	if err != nil {
		t.Fatal(err)
	}
	defer snapshot.close()
	if err := os.Truncate(path, 0); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := snapshot.writeZIP(context.Background(), &output); err == nil {
		t.Fatal("truncated input was accepted")
	}
	if _, err := zip.NewReader(bytes.NewReader(output.Bytes()), int64(output.Len())); err == nil {
		t.Fatal("failed export produced a valid ZIP")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := snapshot.writeZIP(ctx, io.Discard); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled export error=%v", err)
	}
}

func TestResultExportPreservesAlreadyInterruptedStoredState(t *testing.T) {
	server := New(ServerConfig{DataDir: t.TempDir()}, nil)
	resultFixture(t, server, "run-interrupted", "interrupted", time.Now().UTC())
	snapshot, err := server.captureResult("run-interrupted")
	if err != nil {
		t.Fatal(err)
	}
	defer snapshot.close()
	if snapshot.storedState != "interrupted" || snapshot.result.State != "interrupted" || snapshot.result.Active {
		t.Fatalf("original state was inferred incorrectly: %+v", snapshot)
	}
}

func TestResultMetricsUseTheSamePersistedSnapshotAsExportedEvents(t *testing.T) {
	server := New(ServerConfig{DataDir: t.TempDir()}, nil)
	resultFixture(t, server, "run", "completed", time.Now().UTC())
	initial := model.EventBatch{Events: []model.TraceEvent{
		cohortDelivery("first", "topic", "receiver", 12),
		cohortPublish("first", "topic", []string{"receiver"}),
	}}
	if err := server.state.appendEvents(initial); err != nil {
		t.Fatal(err)
	}
	snapshot, err := server.captureResult("run")
	if err != nil {
		t.Fatal(err)
	}
	defer snapshot.close()
	// A transport retry and a new publication arrive after the file boundary.
	if err := server.state.appendEvents(initial); err != nil {
		t.Fatal(err)
	}
	if err := server.state.appendEvents(model.EventBatch{Events: []model.TraceEvent{
		cohortPublish("later", "topic", []string{"receiver", "departed"}),
	}}); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := snapshot.writeZIP(context.Background(), &output); err != nil {
		t.Fatal(err)
	}
	files := decodeResultZIP(t, output.Bytes())
	var exported model.Metrics
	if err := json.Unmarshal(files["metrics.json"], &exported); err != nil {
		t.Fatal(err)
	}
	if exported.Published != 1 || exported.ExpectedDeliveries != 1 || exported.EligibleDeliveries != 1 || exported.AverageLatencyMS != 12 {
		t.Fatalf("exported metrics included later telemetry: %+v", exported)
	}
	rebuilt, err := summarizeRunEvents("run", bytes.NewReader(files["events.jsonl"]))
	if err != nil {
		t.Fatal(err)
	}
	rebuiltJSON, err := json.MarshalIndent(rebuilt, "", "  ")
	if err != nil || !bytes.Equal(bytes.TrimSpace(files["metrics.json"]), rebuiltJSON) {
		t.Fatalf("metrics do not reproduce the exported event bytes: %s, %v", files["metrics.json"], err)
	}
	server.state.mu.RLock()
	live, _ := server.state.runMetrics["run"].summarize("run")
	server.state.mu.RUnlock()
	if live.Published != 2 || live.ExpectedDeliveries != 3 {
		t.Fatalf("later telemetry was not processed independently: %+v", live)
	}
}

func TestResultDownloadRejectsSymlinksAndUnsafeIDs(t *testing.T) {
	for _, target := range []string{"run", "scenario.yaml", "experiment.json", "events.jsonl", "runs"} {
		t.Run(target, func(t *testing.T) {
			server := New(ServerConfig{DataDir: t.TempDir()}, slog.New(slog.NewTextHandler(io.Discard, nil)))
			resultFixture(t, server, "run-safe", "completed", time.Now().UTC())
			outside := t.TempDir()
			secret := filepath.Join(outside, "private")
			if err := os.WriteFile(secret, []byte("PRIVATE CONTENT"), 0o600); err != nil {
				t.Fatal(err)
			}
			runs := filepath.Join(server.config.DataDir, "runs")
			path := filepath.Join(runs, "run-safe", target)
			if target == "run" {
				path = filepath.Join(runs, "run-link")
				secret = filepath.Join(runs, "run-safe")
			} else if target == "runs" {
				path = runs
				if err := os.Rename(runs, runs+"-original"); err != nil {
					t.Fatal(err)
				}
				secret = runs + "-original"
			} else if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
				t.Fatal(err)
			}
			if err := os.Symlink(secret, path); err != nil {
				t.Skipf("symlink creation unavailable: %v", err)
			}
			id := "run-safe"
			if target == "run" {
				id = "run-link"
			}
			response := resultRequest(server, http.MethodGet, "/api/v1/experiments/"+id+"/download")
			if response.Code == http.StatusOK || response.Header().Get("Content-Disposition") != "" || strings.Contains(response.Body.String(), "PRIVATE CONTENT") {
				t.Fatalf("symlink exported: %d %s", response.Code, response.Body)
			}
		})
	}
	server := New(ServerConfig{DataDir: t.TempDir()}, nil)
	for _, id := range []string{"..", "../secret", `..\secret`, "/absolute", "C:private", "run.other", "", strings.Repeat("x", 129)} {
		recorder := httptest.NewRecorder()
		server.handleResultDownload(recorder, httptest.NewRequest(http.MethodGet, "/", nil), id)
		if recorder.Code != http.StatusNotFound {
			t.Fatalf("unsafe id %q status=%d", id, recorder.Code)
		}
	}
}

func TestResultsMethodsEmptyListMissingRunAndExistingStop(t *testing.T) {
	server := New(ServerConfig{DataDir: t.TempDir()}, nil)
	response := resultRequest(server, http.MethodGet, "/api/v1/results")
	if response.Code != http.StatusOK || strings.TrimSpace(response.Body.String()) != "[]" {
		t.Fatalf("empty list=%d %s", response.Code, response.Body)
	}
	for _, request := range []struct {
		method, path string
		status       int
	}{
		{http.MethodGet, "/api/v1/experiments/missing/download", http.StatusNotFound},
		{http.MethodPost, "/api/v1/results", http.StatusMethodNotAllowed},
		{http.MethodPost, "/api/v1/experiments/missing/download", http.StatusMethodNotAllowed},
		{http.MethodGet, "/api/v1/experiments/missing/stop", http.StatusMethodNotAllowed},
		{http.MethodGet, "/api/v1/experiments/missing/unknown", http.StatusNotFound},
	} {
		response := resultRequest(server, request.method, request.path)
		if response.Code != request.status {
			t.Fatalf("%s %s = %d, want %d", request.method, request.path, response.Code, request.status)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	server.cancels["run-stop"] = cancel
	response = resultRequest(server, http.MethodPost, "/api/v1/experiments/run-stop/stop")
	if response.Code != http.StatusAccepted || ctx.Err() != context.Canceled {
		t.Fatalf("existing stop action failed: status=%d error=%v", response.Code, ctx.Err())
	}
}
