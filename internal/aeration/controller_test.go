package aeration

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/wyw14/cry-97/internal/model"
	"github.com/wyw14/cry-97/internal/sensor"
)

func newTestController(t *testing.T, limit int) *Controller {
	t.Helper()
	decoder, err := sensor.NewBatchDecoder(16)
	if err != nil {
		t.Fatal(err)
	}
	book, err := NewWindowBook(limit)
	if err != nil {
		t.Fatal(err)
	}
	fleet, err := NewBlowerFleet([]Blower{{ID: "blower-basin-1", BasinID: "basin-1"}})
	if err != nil {
		t.Fatal(err)
	}
	controller, err := NewController(decoder, book, fleet)
	if err != nil {
		t.Fatal(err)
	}
	return controller
}

func submit(t *testing.T, c *Controller, payload string) (Window, *model.DeviceCommand, error) {
	t.Helper()
	return c.SubmitWindow(context.Background(), model.LineID("line-1"), "basin-1",
		[]byte(payload), 1, time.Date(2026, 9, 30, 10, 0, 0, 0, time.UTC))
}

func TestSingleReadingDoesNotTuneBlower(t *testing.T) {
	c := newTestController(t, 5)
	window, command, err := submit(t, c, `[1.8]`)
	if err != nil {
		t.Fatal(err)
	}
	if command != nil {
		t.Fatalf("one reading must not issue a command, got %+v", command)
	}
	if window.Complete || window.Stable {
		t.Fatalf("incomplete window must not be stable: %+v", window)
	}
	if blowers := c.Blowers(); blowers[0].Frequency != 0 || blowers[0].Running {
		t.Fatalf("blower must remain untouched: %+v", blowers[0])
	}
}

func TestSmallThenLateBatchesOnlyTuneWhenWindowFills(t *testing.T) {
	c := newTestController(t, 3)
	window, command, err := submit(t, c, `[1.8]`)
	if err != nil || command != nil || window.Count != 1 {
		t.Fatalf("first batch should only accumulate, got cmd=%v err=%v", command, err)
	}
	// A late batch arrives with fewer readings than the remaining window.
	window, command, err = submit(t, c, `[1.9,1.7]`)
	if err != nil {
		t.Fatal(err)
	}
	if command == nil {
		t.Fatal("complete stable low-DO window should issue a command")
	}
	if command.Setpoint != 48 {
		t.Fatalf("expected raise to 48 Hz, got %v", command.Setpoint)
	}
	if !window.Complete || !window.Stable || window.Count != 3 {
		t.Fatalf("window should be complete and stable: %+v", window)
	}
	if !strings.Contains(command.Metadata["reason"], "48 Hz") {
		t.Fatalf("command must carry an explainable reason, got %q", command.Metadata["reason"])
	}
	if command.Metadata["window_observations"] != "3" {
		t.Fatalf("metadata should record observation count, got %v", command.Metadata)
	}
}

func TestBadQualityBatchRejectsWholeBatchAndBlocksTuning(t *testing.T) {
	c := newTestController(t, 3)
	if _, _, err := submit(t, c, `[1.8,1.9]`); err != nil {
		t.Fatal(err)
	}
	// One non-finite reading poisons the whole batch; nothing is stored.
	if _, command, err := submit(t, c, `[NaN]`); err == nil || command != nil {
		t.Fatalf("NaN batch must be rejected, got cmd=%v err=%v", command, err)
	}
	// Out-of-range reading likewise rejected.
	if _, command, err := submit(t, c, `[1.8,99]`); err == nil || command != nil {
		t.Fatalf("out-of-range batch must be rejected, got cmd=%v err=%v", command, err)
	}
	window, command, err := submit(t, c, `[1.7]`)
	if err != nil {
		t.Fatal(err)
	}
	if command == nil || command.Setpoint != 48 {
		t.Fatalf("valid completion should now tune to 48 Hz, got cmd=%v err=%v", command, err)
	}
	if window.Count != 3 {
		t.Fatalf("rejected batches must not enter window, count=%d", window.Count)
	}
}

func TestCompleteUnstableWindowDoesNotTune(t *testing.T) {
	c := newTestController(t, 3)
	window, command, err := submit(t, c, `[1.5,2.9,3.4]`)
	if err != nil {
		t.Fatal(err)
	}
	if command != nil {
		t.Fatalf("unstable window must not tune the blower, got %+v", command)
	}
	if !window.Complete || window.Stable {
		t.Fatalf("window should be complete but unstable: %+v", window)
	}
	if blowers := c.Blowers(); blowers[0].Running {
		t.Fatal("blower must stay stopped while trend is unstable")
	}
}

func TestStableWindowFollowsExistingTargetBand(t *testing.T) {
	cases := map[string]struct {
		payload  string
		expected float64
	}{
		"below band raises": {`[1.8,1.9,1.7]`, 48},
		"within band holds": {`[2.6,2.7,2.8]`, 36},
		"above band lowers": {`[3.8,3.9,4.0]`, 28},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			c := newTestController(t, 3)
			_, command, err := submit(t, c, tc.payload)
			if err != nil {
				t.Fatal(err)
			}
			if command == nil || command.Setpoint != tc.expected {
				t.Fatalf("expected %.0f Hz, got cmd=%v", tc.expected, command)
			}
			if strings.TrimSpace(command.Metadata["reason"]) == "" {
				t.Fatal("control decision must be explainable")
			}
		})
	}
}
