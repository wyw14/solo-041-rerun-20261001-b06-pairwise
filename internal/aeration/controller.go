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

// Dissolved oxygen target band (mg/L) and the blower frequencies (Hz)
// mapped to each side of the band. These are the long-standing control
// targets; only a complete, stable window may act on them.
const (
	targetLowDO      = 2.0
	targetHighDO     = 3.5
	holdFrequencyHz  = 36.0
	raiseFrequencyHz = 48.0
	lowerFrequencyHz = 28.0
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
	// Too few readings, late arrivals still filling the window, or a full
	// window whose trend is not yet stable: never adjust the blower.
	if !window.Complete || !window.Stable {
		return window, nil, nil
	}
	target, reason := decideTarget(window.Average)
	command, err := model.NewDeviceCommand(lineID, uuid.New(), "blower-"+basinID, model.DeviceBlower, generation, "set", now)
	if err != nil {
		return Window{}, nil, err
	}
	command.Setpoint = target
	command.Metadata["reason"] = reason
	command.Metadata["do_average_mg_l"] = fmt.Sprintf("%.3f", window.Average)
	command.Metadata["window_observations"] = fmt.Sprintf("%d", window.Count)
	if _, err := c.blowers.Apply(command, now); err != nil {
		return Window{}, nil, err
	}
	return window, &command, nil
}

// decideTarget maps a complete, stable window average onto the existing
// dissolved oxygen target band and returns the blower frequency plus a
// human-readable justification for the command metadata.
func decideTarget(average float64) (float64, string) {
	switch {
	case average < targetLowDO:
		return raiseFrequencyHz, fmt.Sprintf(
			"average dissolved oxygen %.2f mg/L below low target %.1f: raise blower to %.0f Hz",
			average, targetLowDO, raiseFrequencyHz)
	case average > targetHighDO:
		return lowerFrequencyHz, fmt.Sprintf(
			"average dissolved oxygen %.2f mg/L above high target %.1f: lower blower to %.0f Hz",
			average, targetHighDO, lowerFrequencyHz)
	default:
		return holdFrequencyHz, fmt.Sprintf(
			"average dissolved oxygen %.2f mg/L within target band %.1f..%.1f: hold blower at %.0f Hz",
			average, targetLowDO, targetHighDO, holdFrequencyHz)
	}
}

func (c *Controller) Windows() []Window {
	return c.windows.All()
}

func (c *Controller) Blowers() []Blower {
	return c.blowers.Snapshot()
}
