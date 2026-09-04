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
	"time"
)

const resultMetadataLimit = 1 << 20

var errResultNotFound = errors.New("saved result not found")

type savedResult struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	State       string    `json:"state"`
	StartedAt   time.Time `json:"startedAt"`
	FinishedAt  time.Time `json:"finishedAt"`
	Active      bool      `json:"active"`
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
}

func (snapshot *resultSnapshot) close() {
	for _, file := range snapshot.files {
		if file.file != nil {
			_ = file.file.Close()
		}
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
	if result.State == "running" && !result.Active {
		result.State = "interrupted"
	}
	return result, nil
}

func (s *Server) resultActive(id string) bool {
	s.state.mu.RLock()
	defer s.state.mu.RUnlock()
	experiment, exists := s.state.experiments[id]
	return exists && experiment.State == "running"
}

func (s *Server) openResultRuns() (*os.Root, error) {
	data, err := os.OpenRoot(s.config.DataDir)
	if err != nil {
		return nil, err
	}
	defer data.Close()
	return openResultDirectory(data, "runs")
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
		Active: snapshot.result.Active, Partial: snapshot.result.Active || snapshot.result.State == "interrupted",
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

func (w resultContextWriter) Write(data []byte) (int, error) {
	if err := w.ctx.Err(); err != nil {
		return 0, err
	}
	return w.writer.Write(data)
}
