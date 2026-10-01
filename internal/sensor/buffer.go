package sensor

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sync"
)

// DissolvedOxygenMin/Max bound physically plausible DO readings in mg/L.
// Values outside the instrument range are treated as bad-quality samples
// rather than fed into the aeration window.
const (
	DissolvedOxygenMin = 0.0
	DissolvedOxygenMax = 20.0
)

type BatchDecoder struct {
	mu      sync.Mutex
	scratch []*float64
}

func NewBatchDecoder(capacity int) (*BatchDecoder, error) {
	if capacity < 1 {
		return nil, errors.New("decoder capacity must be positive")
	}
	return &BatchDecoder{scratch: make([]*float64, 0, capacity)}, nil
}

// Decode parses a dissolved-oxygen batch. Every element must be present
// (JSON null denotes a missing or bad-quality probe reading), finite and
// within the physical DO range; otherwise the whole batch is rejected so
// one anomalous reading can never drive a blower command.
func (d *BatchDecoder) Decode(payload []byte) ([]float64, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.scratch = d.scratch[:0]
	if err := json.Unmarshal(payload, &d.scratch); err != nil {
		return nil, err
	}
	if len(d.scratch) == 0 {
		return nil, errors.New("sample batch is empty")
	}
	values := make([]float64, 0, len(d.scratch))
	for index, reading := range d.scratch {
		if reading == nil {
			return nil, fmt.Errorf("sample %d is missing or flagged as bad quality", index+1)
		}
		if math.IsNaN(*reading) || math.IsInf(*reading, 0) {
			return nil, fmt.Errorf("sample %d is not a finite reading", index+1)
		}
		if *reading < DissolvedOxygenMin || *reading > DissolvedOxygenMax {
			return nil, fmt.Errorf("sample %d %.3f is outside the dissolved oxygen range %.1f-%.1f mg/L", index+1, *reading, DissolvedOxygenMin, DissolvedOxygenMax)
		}
		values = append(values, *reading)
	}
	return values, nil
}

func CloneValues(values []float64) []float64 {
	if values == nil {
		return nil
	}
	copyOfValues := make([]float64, len(values))
	copy(copyOfValues, values)
	return copyOfValues
}
