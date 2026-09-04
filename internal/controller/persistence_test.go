package controller

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

func TestAtomicMetadataReplacementAndTemporaryFileCleanup(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "experiment.json")
	for _, data := range []string{`{"state":"running"}`, `{"state":"completed"}`} {
		if err := writeFileAtomic(path, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
		stored, err := os.ReadFile(path)
		if err != nil || string(stored) != data {
			t.Fatalf("metadata = %s, read error = %v", stored, err)
		}
	}
	targetDir := filepath.Join(dir, "target-directory")
	if err := os.Mkdir(targetDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeFileAtomic(targetDir, []byte("cannot replace a directory"), 0o644); err == nil {
		t.Fatal("replaced a directory with metadata")
	}
	files, err := filepath.Glob(filepath.Join(dir, ".kpl-metadata-*"))
	if err != nil || len(files) != 0 {
		t.Fatalf("temporary metadata files = %v, error = %v", files, err)
	}
}

func TestLinuxMetadataReadersNeverSeePartialJSON(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("validates Linux rename visibility under concurrent reads")
	}
	path := filepath.Join(t.TempDir(), "experiment.json")
	data := []byte(`{"state":"running","scenario":"` + strings.Repeat("x", 128*1024) + `"}`)
	if err := writeFileAtomic(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	var readers sync.WaitGroup
	readers.Add(1)
	go func() {
		defer readers.Done()
		for {
			select {
			case <-done:
				return
			default:
			}
			current, err := os.ReadFile(path)
			if err != nil || !json.Valid(current) {
				t.Errorf("read incomplete metadata: bytes=%d, error=%v", len(current), err)
				return
			}
		}
	}()
	for i := 0; i < 30; i++ {
		if err := writeFileAtomic(path, data, 0o644); err != nil {
			t.Error(err)
			break
		}
	}
	close(done)
	readers.Wait()
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o644 {
		t.Fatalf("metadata permissions: %v %v", info, err)
	}
}
