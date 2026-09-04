package controller

import (
	"fmt"
	"os"
	"path/filepath"
)

func prepareDataDir(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create Controller data directory: %w", err)
	}
	probe, err := os.CreateTemp(dir, ".kpl-write-check-*")
	if err != nil {
		return fmt.Errorf("Controller data directory is not writable: %w", err)
	}
	defer os.Remove(probe.Name())
	if err := probe.Close(); err != nil {
		return fmt.Errorf("check Controller data directory: %w", err)
	}
	return nil
}

// Replace metadata atomically on the same filesystem so readers and process
// restarts never see the partially written JSON from a truncating overwrite.
// Do not fsync each update: phase dispatch and telemetry share the persistence
// lock, and synchronous storage flushes would add disk latency to experiment
// timing. As with event logs, power-loss durability is left to OS write-back.
func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	file, err := os.CreateTemp(filepath.Dir(path), ".kpl-metadata-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if err := file.Chmod(mode); err != nil {
		_ = file.Close()
		return err
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}
