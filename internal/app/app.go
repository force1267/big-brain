package app

import (
	"context"
	"errors"
	"fmt"

	"github.com/sirupsen/logrus"

	"github.com/force1267/big-brain/internal/config"
	"github.com/force1267/big-brain/internal/logging"
	"github.com/force1267/big-brain/internal/telemetry"
)

var (
	// ErrConfig wraps configuration failures during startup.
	ErrConfig = errors.New("app config stage failed")
	// ErrStartup wraps logging/telemetry failures during startup.
	ErrStartup = errors.New("app startup failed")
)

// Runner runs the whole application until the context is cancelled.
type Runner interface {
	Run(ctx context.Context) error
}

// New wires the real implementations into a Runner.
func New() Runner {
	return runner{
		loader:      config.New(),
		logs:        logging.New(),
		telemetryOf: telemetry.New,
	}
}

type runner struct {
	loader      config.Loader
	logs        logging.Initializer
	telemetryOf func(config.Config) telemetry.Provider
}

var _ Runner = runner{}

func (r runner) Run(ctx context.Context) error {
	cfg, err := r.loader.Load()
	if err != nil {
		return fmt.Errorf("%w: %w", ErrConfig, err)
	}

	if err := r.logs.Init(cfg); err != nil {
		return fmt.Errorf("%w: %w", ErrStartup, err)
	}

	tel := r.telemetryOf(cfg)
	if err := tel.Start(ctx); err != nil {
		return fmt.Errorf("%w: %w", ErrStartup, err)
	}
	defer func() {
		if err := tel.Shutdown(context.WithoutCancel(ctx)); err != nil {
			logrus.WithError(err).Error("telemetry shutdown")
		}
	}()

	logrus.WithField("env", cfg.Env).Info("wrapper started")

	// ponytail: no server yet — product features arrive in the next step;
	// this blocks until the OS signals shutdown.
	<-ctx.Done()
	logrus.Info("wrapper stopped")
	return nil
}
