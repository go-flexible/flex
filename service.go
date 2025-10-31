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
	if len(workers) < 1 {
		return errors.New("need at least 1 worker")
	}

	ctx, cancel := signal.NotifyContext(ctx, os.Interrupt, os.Kill, syscall.SIGTERM)
	defer cancel()

	var (
		errC     = make(chan error, len(workers))
		runErrC  = make(chan error, len(workers))
		haltErrC = make(chan error, len(workers))
	)

	for _, worker := range workers {
		if worker == nil {
			return errors.New("received a nil worker")
		}

		go func(worker Worker) {
			if err := worker.Run(ctx); err != nil {
				runErrC <- err
				cancel()
			}
		}(worker)
	}

loop:
	for {
		select {
		case err, ok := <-haltErrC:
			if ok {
				errC <- err
			}
		case err, ok := <-runErrC:
			if ok {
				errC <- err
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

			break loop
		}
	}

	close(errC)

	var errs []error
	for err := range errC {
		if err != nil {
			errs = append(errs, err)
		}
	}

	if len(errs) > 0 {
		return errors.Join(errs...)
	}

	return nil
}
