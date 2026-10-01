package aeration

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"sync"

	"github.com/wyw14/cry-97/internal/sensor"
)

// StabilityTolerance is the maximum acceptable spread (mg/L) between the
// extremes of a full observation window for the DO level to count as stable.
const StabilityTolerance = 0.35

// Window status reasons, used to explain why a control decision was or
// was not made.
const (
	ReasonAccumulating = "accumulating_samples"
	ReasonUnstable     = "spread_exceeds_tolerance"
	ReasonStable       = "stable"
)

type Window struct {
	BasinID string    `json:"basin_id"`
	Values  []float64 `json:"values"`
	Count   int       `json:"count"`
	Limit   int       `json:"limit"`
	Average float64   `json:"average"`
	Spread  float64   `json:"spread"`
	Stable  bool      `json:"stable"`
	Reason  string    `json:"reason"`
	Message string    `json:"message"`
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

// Store appends the decoded batch to the basin's rolling observation window
// and evaluates the accumulated history. Samples are never replaced: a
// window only becomes stable once it holds a full observation window of
// valid readings whose spread is within tolerance.
func (b *WindowBook) Store(basinID string, values []float64) (Window, error) {
	if basinID == "" || len(values) == 0 {
		return Window{}, errors.New("basin and samples are required")
	}
	b.mu.Lock()
	accumulated := append(b.windows[basinID], values...)
	if overflow := len(accumulated) - b.limit; overflow > 0 {
		accumulated = accumulated[overflow:]
	}
	owned := sensor.CloneValues(accumulated)
	b.windows[basinID] = owned
	b.mu.Unlock()
	return evaluate(basinID, owned, b.limit), nil
}

func (b *WindowBook) All() []Window {
	b.mu.RLock()
	result := make([]Window, 0, len(b.windows))
	for basinID, values := range b.windows {
		result = append(result, evaluate(basinID, append([]float64(nil), values...), b.limit))
	}
	b.mu.RUnlock()
	sort.Slice(result, func(i, j int) bool { return result[i].BasinID < result[j].BasinID })
	return result
}

func evaluate(basinID string, values []float64, limit int) Window {
	window := Window{
		BasinID: basinID,
		Values:  append([]float64(nil), values...),
		Count:   len(values),
		Limit:   limit,
		Reason:  ReasonAccumulating,
	}
	var total float64
	minimum, maximum := math.Inf(1), math.Inf(-1)
	for _, value := range values {
		total += value
		minimum = math.Min(minimum, value)
		maximum = math.Max(maximum, value)
	}
	if len(values) > 0 {
		window.Average = total / float64(len(values))
		window.Spread = maximum - minimum
	}
	switch {
	case len(values) < limit:
		window.Reason = ReasonAccumulating
		window.Message = fmt.Sprintf("%d of %d valid observations; holding frequency", len(values), limit)
	case window.Spread > StabilityTolerance:
		window.Reason = ReasonUnstable
		window.Message = fmt.Sprintf("spread %.2f mg/L exceeds tolerance %.2f; holding frequency", window.Spread, StabilityTolerance)
	default:
		window.Stable = true
		window.Reason = ReasonStable
		window.Message = fmt.Sprintf("full window stable at %.2f mg/L", window.Average)
	}
	return window
}
