package aeration

import (
	"errors"
	"math"
	"sort"
	"sync"

	"github.com/wyw14/cry-97/internal/sensor"
)

// stabilitySpread is the maximum max-min spread (mg/L) a complete window may
// have for the dissolved oxygen trend to be judged stable.
const stabilitySpread = 0.35

type Window struct {
	BasinID string `json:"basin_id"`
	// Values holds the accumulated valid readings of the current window.
	Values []float64 `json:"values"`
	// Count is the number of valid readings accumulated so far.
	Count int `json:"count"`
	// Limit is the minimum number of readings required for a decision.
	Limit int `json:"limit"`
	// Complete reports whether the observation window is fully populated.
	// An incomplete window can never be Stable and must never drive control.
	Complete bool    `json:"complete"`
	Average  float64 `json:"average"`
	Stable   bool    `json:"stable"`
}

type WindowBook struct {
	mu      sync.RWMutex
	limit   int
	windows map[string][]float64
}

func NewWindowBook(limit int) (*WindowBook, error) {
	if limit < 3 {
		return nil, errors.New("aeration window must contain at least three values")
	}
	return &WindowBook{limit: limit, windows: make(map[string][]float64)}, nil
}

func (b *WindowBook) Limit() int {
	return b.limit
}

// Store accumulates the decoded readings for a basin into the current
// observation window. Small and late batches extend the window instead of
// replacing it; only the most recent limit readings are retained. The
// returned window is only considered stable once limit valid readings have
// been observed, so a handful of readings cannot trigger a blower command.
func (b *WindowBook) Store(basinID string, values []float64) (Window, error) {
	if basinID == "" || len(values) == 0 {
		return Window{}, errors.New("basin and samples are required")
	}
	b.mu.Lock()
	existing := b.windows[basinID]
	combined := make([]float64, 0, len(existing)+len(values))
	combined = append(combined, existing...)
	combined = append(combined, values...)
	if len(combined) > b.limit {
		combined = sensor.CloneValues(combined[len(combined)-b.limit:])
	}
	owned := sensor.CloneValues(combined)
	b.windows[basinID] = owned
	b.mu.Unlock()
	return evaluate(basinID, b.limit, owned), nil
}

func (b *WindowBook) All() []Window {
	b.mu.RLock()
	result := make([]Window, 0, len(b.windows))
	for basinID, values := range b.windows {
		result = append(result, evaluate(basinID, b.limit, append([]float64(nil), values...)))
	}
	b.mu.RUnlock()
	sort.Slice(result, func(i, j int) bool { return result[i].BasinID < result[j].BasinID })
	return result
}

func evaluate(basinID string, limit int, values []float64) Window {
	window := Window{
		BasinID: basinID,
		Values:  append([]float64(nil), values...),
		Count:   len(values),
		Limit:   limit,
	}
	if len(values) == 0 {
		return window
	}
	var total float64
	minimum, maximum := math.Inf(1), math.Inf(-1)
	for _, value := range values {
		total += value
		minimum = math.Min(minimum, value)
		maximum = math.Max(maximum, value)
	}
	window.Average = total / float64(len(values))
	window.Complete = len(values) >= limit
	window.Stable = window.Complete && maximum-minimum <= stabilitySpread
	return window
}
