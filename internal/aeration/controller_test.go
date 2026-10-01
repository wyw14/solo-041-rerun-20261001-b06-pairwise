package aeration

import (
	"context"
	"testing"
	"time"

	"github.com/wyw14/cry-97/internal/sensor"
)

var testTime = time.Date(2026, 10, 1, 8, 0, 0, 0, time.UTC)

func newTestController(t *testing.T, limit int) *Controller {
	t.Helper()
	decoder, err := sensor.NewBatchDecoder(limit)
	if err != nil {
		t.Fatalf("decoder: %v", err)
	}
	book, err := NewWindowBook(limit)
	if err != nil {
		t.Fatalf("window book: %v", err)
	}
	fleet, err := NewBlowerFleet([]Blower{
		{ID: "blower-basin-1", BasinID: "basin-1"},
		{ID: "blower-basin-2", BasinID: "basin-2"},
	})
	if err != nil {
		t.Fatalf("fleet: %v", err)
	}
	controller, err := NewController(decoder, book, fleet)
	if err != nil {
		t.Fatalf("controller: %v", err)
	}
	return controller
}

func TestSingleReadingDoesNotCommandBlower(t *testing.T) {
	controller := newTestController(t, 10)
	window, command, err := controller.SubmitWindow(context.Background(), "line-1", "basin-1", []byte(`[1.2]`), 1, testTime)
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if command != nil {
		t.Fatalf("single reading triggered a blower command: %+v", command)
	}
	if window.Stable {
		t.Fatal("window marked stable with 1 of 10 samples")
	}
	if window.Count != 1 || window.Reason != ReasonAccumulating {
		t.Fatalf("unexpected window: %+v", window)
	}
	if got := controller.Blowers()[0].Frequency; got != 0 {
		t.Fatalf("blower frequency changed without command: %v", got)
	}
}

func TestSamplesAccumulateAcrossBatchesUntilFullWindow(t *testing.T) {
	controller := newTestController(t, 5)
	// Three late, small batches arriving separately must accumulate.
	for _, batch := range []string{`[2.5]`, `[2.6]`, `[2.4]`} {
		window, command, err := controller.SubmitWindow(context.Background(), "line-1", "basin-1", []byte(batch), 1, testTime)
		if err != nil {
			t.Fatalf("submit: %v", err)
		}
		if command != nil {
			t.Fatalf("partial window (%d samples) commanded the blower", window.Count)
		}
	}
	// Two more readings complete the window of five; spread is 0.2 <= 0.35.
	window, command, err := controller.SubmitWindow(context.Background(), "line-1", "basin-1", []byte(`[2.5,2.4]`), 1, testTime)
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if !window.Stable || command == nil {
		t.Fatalf("full stable window should command blower: window=%+v command=%v", window, command)
	}
	if command.Setpoint != FrequencyNominal {
		t.Fatalf("DO in band should hold nominal %v Hz, got %v", FrequencyNominal, command.Setpoint)
	}
	if command.Metadata["decision"] != decisionHold {
		t.Fatalf("unexpected decision metadata: %v", command.Metadata)
	}
}

func TestFullWindowUnstableHoldsFrequency(t *testing.T) {
	controller := newTestController(t, 3)
	window, command, err := controller.SubmitWindow(
		context.Background(), "line-1", "basin-1", []byte(`[1.5,2.0,2.6]`), 1, testTime)
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if command != nil {
		t.Fatalf("unstable full window commanded blower: %+v", command)
	}
	if window.Stable || window.Reason != ReasonUnstable {
		t.Fatalf("expected unstable window, got %+v", window)
	}
}

func TestBadQualityBatchIsRejectedAndDoesNotTouchWindow(t *testing.T) {
	controller := newTestController(t, 5)
	if _, _, err := controller.SubmitWindow(
		context.Background(), "line-1", "basin-1", []byte(`[2.5,2.6]`), 1, testTime); err != nil {
		t.Fatalf("good batch: %v", err)
	}
	// A null reading previously decoded to 0 and could drive the blower to 48 Hz.
	if _, _, err := controller.SubmitWindow(
		context.Background(), "line-1", "basin-1", []byte(`[null]`), 1, testTime); err == nil {
		t.Fatal("null reading should be rejected")
	}
	windows := controller.Windows()
	if len(windows) != 1 || windows[0].Count != 2 {
		t.Fatalf("rejected batch must not alter the window: %+v", windows)
	}
	if controller.Blowers()[0].Frequency != 0 {
		t.Fatal("rejected batch changed blower frequency")
	}
}

func TestStableLowDOWindowRaisesFrequency(t *testing.T) {
	controller := newTestController(t, 3)
	window, command, err := controller.SubmitWindow(
		context.Background(), "line-1", "basin-1", []byte(`[1.7,1.8,1.75]`), 1, testTime)
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if !window.Stable || command == nil {
		t.Fatal("expected command on stable low-DO window")
	}
	if command.Setpoint != 48 {
		t.Fatalf("expected 48 Hz for low DO, got %v", command.Setpoint)
	}
	if command.Metadata["decision"] != decisionIncrease {
		t.Fatalf("expected increase_airflow decision, got %v", command.Metadata)
	}
}

func TestStableHighDOWindowLowersFrequency(t *testing.T) {
	controller := newTestController(t, 3)
	_, command, err := controller.SubmitWindow(
		context.Background(), "line-1", "basin-1", []byte(`[3.8,3.9,3.75]`), 1, testTime)
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if command == nil || command.Setpoint != 28 {
		t.Fatalf("expected 28 Hz for high DO, got %+v", command)
	}
}

func TestRollingWindowKeepsLatestSamples(t *testing.T) {
	controller := newTestController(t, 3)
	window, _, err := controller.SubmitWindow(
		context.Background(), "line-1", "basin-1", []byte(`[1.0,1.0]`), 1, testTime)
	if err != nil {
		t.Fatalf("first submit: %v", err)
	}
	if len(window.Values) != 2 || window.Stable {
		t.Fatalf("partial batch should not be stable: %+v", window)
	}
	// After 5 readings the rolling window holds only the latest three.
	window, command, err := controller.SubmitWindow(
		context.Background(), "line-1", "basin-1", []byte(`[2.0,2.0,2.0]`), 1, testTime)
	if err != nil {
		t.Fatalf("second submit: %v", err)
	}
	if len(window.Values) != 3 || window.Values[0] != 2.0 {
		t.Fatalf("window should hold latest three readings: %v", window.Values)
	}
	if command == nil || command.Setpoint != FrequencyNominal {
		t.Fatalf("latest rolling window is stable in band, expected hold command: %+v", command)
	}
}

func TestBasinsAccumulateIndependently(t *testing.T) {
	controller := newTestController(t, 3)
	if _, _, err := controller.SubmitWindow(
		context.Background(), "line-1", "basin-1", []byte(`[2.5]`), 1, testTime); err != nil {
		t.Fatalf("basin-1: %v", err)
	}
	window, command, err := controller.SubmitWindow(
		context.Background(), "line-1", "basin-2", []byte(`[2.5,2.5,2.5]`), 1, testTime)
	if err != nil {
		t.Fatalf("basin-2: %v", err)
	}
	if command == nil || !window.Stable {
		t.Fatal("basin-2 should evaluate on its own full window")
	}
	for _, w := range controller.Windows() {
		if w.BasinID == "basin-1" && (w.Count != 1 || w.Stable) {
			t.Fatalf("basin-1 samples leaked across basins: %+v", w)
		}
	}
}

func TestUnknownBlowerTargetErrors(t *testing.T) {
	controller := newTestController(t, 3)
	_, command, err := controller.SubmitWindow(
		context.Background(), "line-1", "basin-unknown", []byte(`[2.5,2.5,2.5]`), 1, testTime)
	if err == nil {
		t.Fatal("unknown blower should error")
	}
	if command != nil {
		t.Fatalf("unknown blower should not yield a command: %+v", command)
	}
}
