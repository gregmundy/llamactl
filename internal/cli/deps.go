package cli

import (
	"context"
	"errors"
	"io"
	"time"

	"github.com/gregmundy/llamactl/internal/hardware"
)

var ErrUserError = errors.New("user error")

// HardwareDetector matches *hardware.Detector. Tests substitute a fake.
type HardwareDetector interface {
	Detect(ctx context.Context) (hardware.Info, error)
}

type Deps struct {
	Stdout io.Writer
	Stderr io.Writer

	HardwareDetector HardwareDetector
	HardwareJSONPath string

	Now func() time.Time
}
