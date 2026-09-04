package controller

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"
)

const resultMetadataLimit = 1 << 20

var (
	errResultNotFound = errors.New("saved result not found")
	errResultBusy     = errors.New("saved result is still active, queued, finalizing, or being downloaded")
)

type savedResult struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	State       string    `json:"state"`
	StartedAt   time.Time `json:"startedAt"`
	FinishedAt  time.Time `json:"finishedAt"`
	Active      bool      `json:"active"`
	BatchID     string    `json:"batchId,omitempty"`
	Iteration   int       `json:"iteration,omitempty"`
	Repetitions int       `json:"repetitions,omitempty"`
	storedState string
}

type resultFile struct {
	name string
	file *os.File
	size int64
}

type resultSnapshot struct {
	files       []resultFile
	exportedAt  time.Time
	active      bool
	result      savedResult
	storedState string
	release     func()
}

func (snapshot *resultSnapshot) close() {
	for _, file := range snapshot.files {
		if file.file != nil {
			_ = file.file.Close()
		}
	}
	if snapshot.release != nil {
		snapshot.release()
		snapshot.release = nil
	}
}

// Result IDs are directory names, never paths or names normalized into paths.
func validResultID(id string) bool {
	if len(id) == 0 || len(id) > 128 {
		return false
	}
	for _, ch := range id {
		if ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '-' || ch == '_' {
			continue
		}
		return false
	}
	return true
}

// Root confines resolution even if a path is replaced during these checks.
// Reject links inside that boundary too: a run must not alias another run.
func openResultDirectory(parent *os.Root, name string) (*os.Root, error) {
	info, err := parent.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("%s is not a regular result directory", name)
	}
	root, err := parent.OpenRoot(name)
	if err != nil {
		return nil, err
	}
	opened, err := root.Stat(".")
	if err != nil || !os.SameFile(info, opened) {
		_ = root.Close()
		return nil, fmt.Errorf("result directory changed while opening %s", name)
	}
	return root, nil
}

func openResultFile(root *os.Root, name string) (resultFile, error) {
	result := resultFile{name: name}
	info, err := root.Lstat(name)
	if err != nil {
		return result, err
	}
	if !info.Mode().IsRegular() {
		return result, fmt.Errorf("%s is not a regular result file", name)
	}
	file, err := root.Open(name)
	if err != nil {
		return result, err
	}
	opened, err := file.Stat()
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(info, opened) {
		_ = file.Close()
		return result, fmt.Errorf("result file changed while opening %s", name)
	}
	result.file, result.size = file, opened.Size()
	return result, nil
}

func readResultMetadata(file resultFile, id string, active bool) (savedResult, error) {
	var result savedResult
	if file.size > resultMetadataLimit {
		return result, fmt.Errorf("experiment.json exceeds %d bytes", resultMetadataLimit)
	}
	decoder := json.NewDecoder(io.NewSectionReader(file.file, 0, file.size))
	if err := decoder.Decode(&result); err != nil {
		return result, fmt.Errorf("decode experiment.json: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return result, errors.New("experiment.json contains trailing data")
	}
	if result.ID != id || result.State == "" {
		return result, errors.New("experiment.json has an invalid ID or state")
	}
	// Persisted data cannot claim ownership by the current Controller process.
	result.storedState = result.State
	result.Active = active && result.State == "running"
	if (result.State == "running" || result.State == "queued") && !active {
		result.State = "interrupted"
	}
	return result, nil
}

func (s *Server) resultActive(id string) bool {
	s.state.mu.RLock()
	defer s.state.mu.RUnlock()
	experiment, exists := s.state.experiments[id]
	return exists && (experiment.State == "running" || experiment.State == "queued")
}

func (s *Server) openResultRuns() (*os.Root, error) {
	data, err := os.OpenRoot(s.config.DataDir)
	if err != nil {
		return nil, err
	}
	defer data.Close()
	return openResultDirectory(data, "runs")
}

// The marker survives Controller restarts. Persistence callers hold persistMu
// so a late Agent batch cannot recreate a directory after its deletion.
func (s *state) resultDeletedLocked(id string) (bool, error) {
	if !validResultID(id) {
		return false, errors.New("invalid result id")
	}
	data, err := os.OpenRoot(s.dataDir)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer data.Close()
	markers, err := openResultDirectory(data, ".deleted-results")
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer markers.Close()
	info, err := markers.Lstat(id)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.Mode().IsRegular() {
		return false, errors.New("deletion marker is not a regular file")
	}
	return true, nil
}

func (s *Server) markResultDeletedLocked(id string) error {
	data, err := os.OpenRoot(s.config.DataDir)
	if err != nil {
		return err
	}
	defer data.Close()
	if err := data.Mkdir(".deleted-results", 0o700); err != nil && !errors.Is(err, os.ErrExist) {
		return err
	}
	markers, err := openResultDirectory(data, ".deleted-results")
	if err != nil {
		return err
	}
	defer markers.Close()
	file, err := markers.OpenFile(id, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		_, err = s.state.resultDeletedLocked(id)
		return err
	}
	if err != nil {
		return err
	}
	writeErr := json.NewEncoder(file).Encode(map[string]any{"id": id, "deletedAt": time.Now().UTC()})
	return errors.Join(writeErr, file.Close())
}

// Remove entries relative to held directory descriptors. Symlinks are unlinked,
// never traversed. This uses only Root methods available in Go 1.24.
func removeResultDirectory(parent *os.Root, id string) error {
	directory, err := openResultDirectory(parent, id)
	if err != nil {
		return err
	}
	file, err := directory.Open(".")
	if err != nil {
		_ = directory.Close()
		return err
	}
	entries, readErr := file.ReadDir(-1)
	_ = file.Close()
	if readErr != nil {
		_ = directory.Close()
		return readErr
	}
	for _, entry := range entries {
		if entry.IsDir() {
			err = removeResultDirectory(directory, entry.Name())
		} else {
			err = directory.Remove(entry.Name())
		}
		if err != nil {
			_ = directory.Close()
			return err
		}
	}
	if err := directory.Close(); err != nil {
		return err
	}
	return parent.Remove(id)
}

func (s *Server) handleResultAction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		methodNotAllowed(w)
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/v1/results/")
	if !validResultID(id) {
		http.NotFound(w, r)
		return
	}
	if err := s.deleteSavedResult(id); err != nil {
		switch {
		case errors.Is(err, errResultNotFound):
			http.NotFound(w, r)
		case errors.Is(err, errResultBusy):
			writeError(w, http.StatusConflict, err.Error())
		default:
			s.logger.Error("delete saved result", "run", id, "error", err)
			writeError(w, http.StatusInternalServerError, "cannot delete saved result; retry after resolving the storage error")
		}
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) deleteSavedResult(id string) error {
	if !validResultID(id) {
		return errResultNotFound
	}
	s.cancelMu.Lock()
	defer s.cancelMu.Unlock()
	s.state.persistMu.Lock()
	defer s.state.persistMu.Unlock()
	s.state.mu.RLock()
	experiment := s.state.experiments[id]
	s.state.mu.RUnlock()
	if experiment.State == "running" || experiment.State == "queued" || s.cancels[id] != nil || s.repeatBatches[id] != nil || s.resultDownloads[id] > 0 {
		return errResultBusy
	}
	runs, err := s.openResultRuns()
	if errors.Is(err, os.ErrNotExist) {
		return errResultNotFound
	}
	if err != nil {
		return err
	}
	defer runs.Close()
	directory, err := openResultDirectory(runs, id)
	if errors.Is(err, os.ErrNotExist) {
		return errResultNotFound
	}
	if err != nil {
		return err
	}
	_ = directory.Close()
	if err := s.markResultDeletedLocked(id); err != nil {
		return fmt.Errorf("persist deletion marker: %w", err)
	}
	if err := removeResultDirectory(runs, id); err != nil {
		return err
	}
	s.state.mu.Lock()
	delete(s.state.experiments, id)
	delete(s.state.runMetrics, id)
	events := s.state.events[:0]
	for _, event := range s.state.events {
		if event.RunID != id {
			events = append(events, event)
		}
	}
	s.state.events = events
	s.state.mu.Unlock()
	s.state.notify()
	return nil
}

func (s *Server) handleResults(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	results := make([]savedResult, 0)
	runs, err := s.openResultRuns()
	if errors.Is(err, os.ErrNotExist) {
		writeJSON(w, http.StatusOK, results)
		return
	}
	if err != nil {
		s.logger.Warn("list saved results", "error", err)
		writeError(w, http.StatusInternalServerError, "cannot read saved results")
		return
	}
	defer runs.Close()
	directory, err := runs.Open(".")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "cannot read saved results")
		return
	}
	defer directory.Close()
	for {
		entries, readErr := directory.ReadDir(100)
		for _, entry := range entries {
			if !validResultID(entry.Name()) || !entry.IsDir() {
				s.logger.Warn("skip unsafe saved result entry", "entry", entry.Name())
				continue
			}
			id := entry.Name()
			result, err := s.readSavedResult(runs, id)
			if errors.Is(err, errResultNotFound) {
				continue
			}
			if err != nil {
				s.logger.Warn("read saved result metadata", "run", id, "error", err)
				result = savedResult{ID: id, Name: id, State: "unreadable"}
			}
			results = append(results, result)
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			s.logger.Warn("list saved results", "error", readErr)
			writeError(w, http.StatusInternalServerError, "cannot read saved results")
			return
		}
		if err := r.Context().Err(); err != nil {
			return
		}
	}
	sort.Slice(results, func(i, j int) bool {
		if results[i].StartedAt.Equal(results[j].StartedAt) {
			return results[i].ID < results[j].ID
		}
		return results[i].StartedAt.After(results[j].StartedAt)
	})
	writeJSON(w, http.StatusOK, results)
}

func (s *Server) readSavedResult(runs *os.Root, id string) (savedResult, error) {
	s.state.persistMu.Lock()
	deleted, err := s.state.resultDeletedLocked(id)
	if err != nil || deleted {
		s.state.persistMu.Unlock()
		if err != nil {
			return savedResult{}, err
		}
		return savedResult{}, errResultNotFound
	}
	root, err := openResultDirectory(runs, id)
	var file resultFile
	if err == nil {
		file, err = openResultFile(root, "experiment.json")
		_ = root.Close()
	}
	active := s.resultActive(id)
	s.state.persistMu.Unlock()
	if err != nil {
		return savedResult{}, err
	}
	defer file.file.Close()
	return readResultMetadata(file, id, active)
}

func (s *Server) captureResult(id string) (*resultSnapshot, error) {
	if !validResultID(id) {
		return nil, errResultNotFound
	}
	runs, err := s.openResultRuns()
	if errors.Is(err, os.ErrNotExist) {
		return nil, errResultNotFound
	}
	if err != nil {
		return nil, err
	}
	defer runs.Close()
	snapshot := &resultSnapshot{}
	// Event appends and metadata replacements use this same lock. Only open,
	// stat and the in-memory ownership check belong in the critical section.
	err = func() error {
		s.state.persistMu.Lock()
		defer s.state.persistMu.Unlock()
		deleted, err := s.state.resultDeletedLocked(id)
		if err != nil {
			return err
		}
		if deleted {
			return errResultNotFound
		}
		root, err := openResultDirectory(runs, id)
		if errors.Is(err, os.ErrNotExist) {
			return errResultNotFound
		}
		if err != nil {
			return err
		}
		defer root.Close()
		for _, name := range []string{"scenario.yaml", "experiment.json", "events.jsonl"} {
			file, err := openResultFile(root, name)
			if err != nil && !(name == "events.jsonl" && errors.Is(err, os.ErrNotExist)) {
				return fmt.Errorf("open %s: %w", name, err)
			}
			snapshot.files = append(snapshot.files, file)
		}
		snapshot.active = s.resultActive(id)
		snapshot.exportedAt = time.Now().UTC()
		if s.resultDownloads == nil {
			s.resultDownloads = make(map[string]int)
		}
		s.resultDownloads[id]++
		snapshot.release = func() {
			s.state.persistMu.Lock()
			s.resultDownloads[id]--
			if s.resultDownloads[id] == 0 {
				delete(s.resultDownloads, id)
			}
			s.state.persistMu.Unlock()
		}
		return nil
	}()
	if err != nil {
		snapshot.close()
		return nil, err
	}
	result, err := readResultMetadata(snapshot.files[1], id, snapshot.active)
	if err == nil {
		// Probe both ends before committing HTTP headers. Later I/O failures
		// still abort the response instead of finishing a misleading ZIP.
		for _, file := range snapshot.files {
			if file.size == 0 {
				continue
			}
			var probe [1]byte
			if _, err = file.file.ReadAt(probe[:], 0); err != nil {
				break
			}
			if _, err = file.file.ReadAt(probe[:], file.size-1); err != nil {
				break
			}
		}
	}
	if err != nil {
		snapshot.close()
		return nil, err
	}
	snapshot.result = result
	snapshot.storedState = result.storedState
	return snapshot, nil
}

func (s *Server) handleResultDownload(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	snapshot, err := s.captureResult(id)
	if err != nil {
		if errors.Is(err, errResultNotFound) {
			http.NotFound(w, r)
			return
		}
		s.logger.Warn("capture saved result", "run", id, "error", err)
		writeError(w, http.StatusInternalServerError, "saved result files are unreadable")
		return
	}
	defer snapshot.close()
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s.zip\"", id))
	if err := snapshot.writeZIP(r.Context(), w); err != nil {
		s.logger.Warn("stream saved result", "run", id, "error", err)
		// A partial archive must not receive a valid central directory. Tell
		// net/http to abort the connection/stream without a redundant panic log.
		panic(http.ErrAbortHandler)
	}
}

func (snapshot *resultSnapshot) writeZIP(ctx context.Context, output io.Writer) error {
	archive := zip.NewWriter(resultContextWriter{ctx: ctx, writer: output})
	sizes := make(map[string]int64, len(snapshot.files))
	for _, file := range snapshot.files {
		if err := ctx.Err(); err != nil {
			return err
		}
		entry, err := archive.Create(file.name)
		if err != nil {
			return err
		}
		if file.file != nil {
			if _, err := io.CopyN(entry, io.NewSectionReader(file.file, 0, file.size), file.size); err != nil {
				return fmt.Errorf("copy %s: %w", file.name, err)
			}
		}
		sizes[file.name] = file.size
	}
	var eventLog io.Reader = strings.NewReader("")
	for _, file := range snapshot.files {
		if file.name == "events.jsonl" && file.file != nil {
			eventLog = io.NewSectionReader(file.file, 0, file.size)
			break
		}
	}
	metrics, err := summarizeRunEvents(snapshot.result.ID, resultContextReader{ctx: ctx, reader: eventLog}, snapshot.exportedAt)
	if err != nil {
		return fmt.Errorf("summarize saved events: %w", err)
	}
	metricsJSON, err := json.MarshalIndent(metrics, "", "  ")
	if err != nil {
		return err
	}
	metricsJSON = append(metricsJSON, '\n')
	metricsEntry, err := archive.Create("metrics.json")
	if err != nil {
		return err
	}
	if _, err := metricsEntry.Write(metricsJSON); err != nil {
		return err
	}
	sizes["metrics.json"] = int64(len(metricsJSON))
	exported, err := archive.Create("export.json")
	if err != nil {
		return err
	}
	if err := json.NewEncoder(exported).Encode(struct {
		Version     int              `json:"version"`
		RunID       string           `json:"runId"`
		ExportedAt  time.Time        `json:"exportedAt"`
		Active      bool             `json:"active"`
		Partial     bool             `json:"partial"`
		State       string           `json:"state"`
		StoredState string           `json:"storedState"`
		EventBytes  int64            `json:"eventBytes"`
		Files       map[string]int64 `json:"files"`
		Boundary    string           `json:"boundary"`
	}{
		Version: 1, RunID: snapshot.result.ID, ExportedAt: snapshot.exportedAt,
		Active: snapshot.result.Active, Partial: snapshot.result.Active || snapshot.result.State == "interrupted" || snapshot.result.State == "queued",
		State: snapshot.result.State, StoredState: snapshot.storedState,
		EventBytes: sizes["events.jsonl"], Files: sizes,
		Boundary: "Only bytes persisted at the snapshot boundary are included. In-flight or subsequently received telemetry is excluded; a finished experiment may still receive late telemetry.",
	}); err != nil {
		return err
	}
	return archive.Close()
}

type resultContextWriter struct {
	ctx    context.Context
	writer io.Writer
}

type resultContextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r resultContextReader) Read(data []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(data)
}

func (w resultContextWriter) Write(data []byte) (int, error) {
	if err := w.ctx.Err(); err != nil {
		return 0, err
	}
	return w.writer.Write(data)
}
