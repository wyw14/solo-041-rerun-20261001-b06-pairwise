package aeration

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/wyw14/cry-97/internal/model"
	"github.com/wyw14/cry-97/internal/sensor"
)

// Dissolved-oxygen target band (mg/L) and the blower frequencies (Hz)
// commanded outside it. A reading inside the band holds the nominal
// frequency.
const (
	TargetLow        = 2.0
	TargetHigh       = 3.5
	FrequencyLowDO   = 48.0
	FrequencyHighDO  = 28.0
	FrequencyNominal = 36.0
)

const (
	decisionIncrease = "increase_airflow"
	decisionDecrease = "decrease_airflow"
	decisionHold     = "hold_frequency"
)

type Controller struct {
	decoder *sensor.BatchDecoder
	windows *WindowBook
	blowers *BlowerFleet
}

func NewController(decoder *sensor.BatchDecoder, windows *WindowBook, blowers *BlowerFleet) (*Controller, error) {
	if decoder == nil || windows == nil || blowers == nil {
		return nil, errors.New("aeration controller dependencies are required")
	}
	return &Controller{decoder: decoder, windows: windows, blowers: blowers}, nil
}

// SubmitWindow decodes one DO batch, accumulates it into the basin's
// observation window, and only commands the blower once the window holds
// enough valid, stable observations. While samples are still accumulating
// or the window is unstable, no command is issued, regardless of how
// extreme the few readings seen so far are.
func (c *Controller) SubmitWindow(ctx context.Context, lineID model.LineID, basinID string, payload []byte, generation uint64, now time.Time) (Window, *model.DeviceCommand, error) {
	if err := ctx.Err(); err != nil {
		return Window{}, nil, err
	}
	values, err := c.decoder.Decode(payload)
	if err != nil {
		return Window{}, nil, err
	}
	window, err := c.windows.Store(basinID, values)
	if err != nil {
		return Window{}, nil, err
	}
	if !window.Stable {
		// Few, late, or spread-out readings: wait for a full valid window.
		return window, nil, nil
	}
	target, decision := decideFrequency(window.Average)
	command, err := model.NewDeviceCommand(lineID, uuid.New(), "blower-"+basinID, model.DeviceBlower, generation, "set", now)
	if err != nil {
		return Window{}, nil, err
	}
	command.Setpoint = target
	command.Metadata = map[string]string{
		"window_count":    fmt.Sprintf("%d", window.Count),
		"window_average":  fmt.Sprintf("%.3f", window.Average),
		"window_spread":   fmt.Sprintf("%.3f", window.Spread),
		"decision":        decision,
		"decision_reason": decisionReason(decision, window.Average),
	}
	if _, err := c.blowers.Apply(command, now); err != nil {
		return Window{}, nil, err
	}
	return window, &command, nil
}

func decideFrequency(average float64) (float64, string) {
	switch {
	case average < TargetLow:
		return FrequencyLowDO, decisionIncrease
	case average > TargetHigh:
		return FrequencyHighDO, decisionDecrease
	default:
		return FrequencyNominal, decisionHold
	}
}

func decisionReason(decision string, average float64) string {
	switch decision {
	case decisionIncrease:
		return fmt.Sprintf("average DO %.2f mg/L below target band %.1f-%.1f; raising blower to %.0f Hz", average, TargetLow, TargetHigh, FrequencyLowDO)
	case decisionDecrease:
		return fmt.Sprintf("average DO %.2f mg/L above target band %.1f-%.1f; lowering blower to %.0f Hz", average, TargetLow, TargetHigh, FrequencyHighDO)
	default:
		return fmt.Sprintf("average DO %.2f mg/L inside target band %.1f-%.1f; holding blower at %.0f Hz", average, TargetLow, TargetHigh, FrequencyNominal)
	}
}

func (c *Controller) Windows() []Window {
	return c.windows.All()
}

func (c *Controller) Blowers() []Blower {
	return c.blowers.Snapshot()
}
