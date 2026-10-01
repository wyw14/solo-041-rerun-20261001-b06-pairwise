package sensor

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sync"
)

// Dissolved oxygen probe operating range in mg/L. Readings outside the
// physical probe range are bad-quality samples and must never reach a
// control decision.
const (
	MinDissolvedOxygen = 0.0
	MaxDissolvedOxygen = 20.0
)

type BatchDecoder struct {
	mu      sync.Mutex
	scratch []float64
}

func NewBatchDecoder(capacity int) (*BatchDecoder, error) {
	if capacity < 1 {
		return nil, errors.New("decoder capacity must be positive")
	}
	return &BatchDecoder{scratch: make([]float64, 0, capacity)}, nil
}

// ValidateReading rejects non-finite and physically impossible dissolved
// oxygen readings.
func ValidateReading(value float64) error {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return errors.New("sample reading is not finite")
	}
	if value < MinDissolvedOxygen || value > MaxDissolvedOxygen {
		return fmt.Errorf("sample reading %.3f is outside %.1f..%.1f mg/L", value, MinDissolvedOxygen, MaxDissolvedOxygen)
	}
	return nil
}

// Decode parses a batch of dissolved oxygen readings. Null entries are
// rejected instead of being silently coerced to zero, and every reading is
// quality-checked before it can accumulate into an aeration window.
func (d *BatchDecoder) Decode(payload []byte) ([]float64, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	// Decode into pointers so a null reading is detected rather than
	// coerced to 0.0 (which would look like critically low oxygen).
	var wire []*float64
	if err := json.Unmarshal(payload, &wire); err != nil {
		return nil, err
	}
	if len(wire) == 0 {
		return nil, errors.New("sample batch is empty")
	}
	d.scratch = d.scratch[:0]
	if cap(d.scratch) < len(wire) {
		d.scratch = make([]float64, 0, len(wire))
	}
	for index, reading := range wire {
		if reading == nil {
			return nil, fmt.Errorf("sample reading at index %d is missing", index)
		}
		if err := ValidateReading(*reading); err != nil {
			return nil, fmt.Errorf("sample reading at index %d: %w", index, err)
		}
		d.scratch = append(d.scratch, *reading)
	}
	return CloneValues(d.scratch), nil
}

func CloneValues(values []float64) []float64 {
	if values == nil {
		return nil
	}
	copyOfValues := make([]float64, len(values))
	copy(copyOfValues, values)
	return copyOfValues
}
