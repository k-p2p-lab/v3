package controller

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
)

type cancelAtEndReader struct {
	reader io.Reader
	cancel context.CancelFunc
}

func (r cancelAtEndReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	if errors.Is(err, io.EOF) {
		r.cancel()
	}
	return n, err
}

func TestArchiveMetricSummaryHonorsCancellationAfterLastRead(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// The context-aware file reader already handed Scanner a clean EOF. The
	// remaining CPU aggregation must still see cancellation, without more I/O.
	reader := cancelAtEndReader{reader: strings.NewReader("{\"runId\":\"run\",\"type\":\"publish\"}\n"), cancel: cancel}
	if _, err := summarizeRunEventsContext(ctx, "run", reader); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled aggregation returned %v", err)
	}
}
