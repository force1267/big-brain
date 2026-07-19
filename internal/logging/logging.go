package logging

import (
	"errors"
	"fmt"

	"github.com/sirupsen/logrus"

	"github.com/force1267/big-brain/internal/config"
)

var (
	// ErrInit wraps any failure while configuring logging.
	ErrInit = errors.New("logging init failed")
	// ErrBadLevel is returned when the configured level is not a logrus level.
	ErrBadLevel = errors.New("unknown log level")
)

// Initializer applies logging configuration to the process-wide logger.
type Initializer interface {
	Init(cfg config.Config) error
}

// New returns the logrus-backed Initializer.
func New() Initializer { return logrusInit{} }

type logrusInit struct{}

var _ Initializer = logrusInit{}

func (logrusInit) Init(cfg config.Config) error {
	lvl, err := logrus.ParseLevel(cfg.Log.Level)
	if err != nil {
		return fmt.Errorf("%w: %w: %q", ErrInit, ErrBadLevel, cfg.Log.Level)
	}
	logrus.SetLevel(lvl)

	if cfg.Log.Format == "json" {
		logrus.SetFormatter(&logrus.JSONFormatter{})
	} else {
		logrus.SetFormatter(&logrus.TextFormatter{})
	}
	return nil
}
