package flex

import (
	"context"
	"errors"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

var logger = log.New(os.Stderr, "flex: ", 0)

// SetLogger sets the package-level logger used by MustStart.
func SetLogger(l *log.Logger) {
	logger = l
}

// Option configures Start behavior.
type Option func(*options)

type options struct {
	noSignals bool
}

// WithoutSignals disables signal-based shutdown via signal.NotifyContext.
// Useful when running inside a testing/synctest bubble or when signal
// handling is managed externally.
func WithoutSignals() Option {
	return func(o *options) {
		o.noSignals = true
	}
}

// DefaultHaltTimeout is the default timeout for graceful shutdown of workers.
// This provides workers with a grace period to close connections, flush data,
// and clean up resources during the halt phase.
const DefaultHaltTimeout = 30 * time.Second

// Runner represents the behaviour for running a service worker.
type Runner interface {
	// Run should run start processing the worker and be a blocking operation.
	Run(context.Context) error
}

// Halter represents the behaviour for stopping a service worker.
type Halter interface {
	// Halt should tell the worker to stop doing work.
	Halt(context.Context) error
}

// Worker represents the behaviour for a service worker.
type Worker interface {
	Runner
	Halter
}

// MustStart is like Start, but panics if there is an error.
func MustStart(ctx context.Context, workers ...Worker) {
	if err := Start(ctx, workers...); err != nil {
		logger.Fatal(err)
	}
}

// Start is a blocking operation that will start processing the workers.
//
// Workers are started concurrently and run until one of the following occurs:
// 1. A worker returns an error from Run()
// 2. A signal (SIGINT, SIGKILL, SIGTERM) is received
// 3. The provided context is canceled
//
// When shutdown is triggered, all workers' Halt() methods are called concurrently
// with a fresh context that has DefaultHaltTimeout as its deadline. This ensures
// workers have a grace period to perform graceful shutdown operations such as
// closing connections or flushing data.
func Start(ctx context.Context, workers ...Worker) error {
	return start(ctx, workers, nil)
}

// StartOpts is like Start but accepts configuration options.
func StartOpts(ctx context.Context, workers []Worker, opts ...Option) error {
	var o options
	for _, opt := range opts {
		opt(&o)
	}
	return start(ctx, workers, &o)
}

func start(ctx context.Context, workers []Worker, opts *options) error {
	if len(workers) < 1 {
		return errors.New("need at least 1 worker")
	}

	var cancel context.CancelFunc
	if opts != nil && opts.noSignals {
		ctx, cancel = context.WithCancel(ctx)
	} else {
		ctx, cancel = signal.NotifyContext(ctx, os.Interrupt, os.Kill, syscall.SIGTERM)
	}
	defer cancel()

	var (
		errC     = make(chan error, len(workers))
		runErrC  = make(chan error, len(workers))
		haltErrC = make(chan error, len(workers))
		runWg    sync.WaitGroup
	)

	runWg.Add(len(workers))
	for _, worker := range workers {
		if worker == nil {
			return errors.New("received a nil worker")
		}

		go func(worker Worker) {
			defer runWg.Done()
			if err := worker.Run(ctx); err != nil {
				runErrC <- err
				cancel()
			}
		}(worker)
	}

	allDone := make(chan struct{})
	go func() {
		runWg.Wait()
		close(allDone)
	}()

	var errs []error

loop:
	for {
		select {
		case err, ok := <-runErrC:
			if ok {
				errC <- err
			}
		case <-allDone:
			if ctx.Err() != nil {
				// Context was canceled (by error or signal); wait for
				// ctx.Done() to trigger the halt phase.
				allDone = nil
			} else {
				break loop
			}
		case <-ctx.Done():
			// Create a fresh context for halt operations with its own timeout.
			// This ensures workers have a grace period to shutdown gracefully,
			// even if they were interrupted by a signal or another worker failure.
			haltCtx, haltCancel := context.WithTimeout(
				context.Background(),
				DefaultHaltTimeout,
			)
			defer haltCancel()

			var wg sync.WaitGroup
			wg.Add(len(workers))

			for _, worker := range workers {
				go func(worker Worker) {
					defer wg.Done()
					err := worker.Halt(haltCtx)
					haltErrC <- err
				}(worker)
			}

			wg.Wait()
			close(haltErrC)
			for err := range haltErrC {
				if err != nil {
					errs = append(errs, err)
				}
			}
			break loop
		}
	}

	close(errC)

	for err := range errC {
		if err != nil {
			errs = append(errs, err)
		}
	}

	// Drain any pending run errors that weren't consumed in the select loop.
drainLoop:
	for {
		select {
		case err := <-runErrC:
			if err != nil {
				errs = append(errs, err)
			}
		default:
			break drainLoop
		}
	}

	if len(errs) > 0 {
		return errors.Join(errs...)
	}

	return nil
}
