package model

import (
	"encoding/json"
	"fmt"
)

const (
	MaxEventBatchBytes  = 10 << 20
	MaxEventBatchEvents = 5000
)

// MarshalEventBatchPrefix encodes the longest source-ordered prefix that fits
// both receiver limits, including JSON escaping and the batch envelope. The
// caller must retain any suffix and only remove this prefix after acceptance.
func MarshalEventBatchPrefix(agentID string, events []TraceEvent) ([]byte, int, error) {
	agent, err := json.Marshal(agentID)
	if err != nil {
		return nil, 0, err
	}
	data := append([]byte(`{"agentId":`), agent...)
	data = append(data, `,"events":[`...)
	if len(data)+2 > MaxEventBatchBytes {
		return nil, 0, fmt.Errorf("event batch envelope exceeds %d bytes", MaxEventBatchBytes)
	}
	count := 0
	for _, event := range events[:min(len(events), MaxEventBatchEvents)] {
		encoded, err := json.Marshal(event)
		if err != nil {
			if count > 0 {
				break
			}
			return nil, 0, fmt.Errorf("encode telemetry event: %w", err)
		}
		separator := 0
		if count > 0 {
			separator = 1
		}
		if len(data)+separator+len(encoded)+2 > MaxEventBatchBytes {
			if count > 0 {
				break
			}
			return nil, 0, fmt.Errorf("encoded telemetry event exceeds %d-byte batch limit", MaxEventBatchBytes)
		}
		if count > 0 {
			data = append(data, ',')
		}
		data = append(data, encoded...)
		count++
	}
	return append(data, ']', '}'), count, nil
}

// ValidateEventSizes checks every event before admitting any part of a batch.
// A body below the input limit can still expand past it when JSON is re-encoded.
func (batch EventBatch) ValidateEventSizes() error {
	for pending := batch.Events; len(pending) > 0; {
		_, count, err := MarshalEventBatchPrefix(batch.AgentID, pending)
		if err != nil {
			return err
		}
		pending = pending[count:]
	}
	return nil
}
