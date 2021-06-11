package flex

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"
)

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

var logger = log.New(os.Stderr, "flex: ", 0)

func MustStart(ctx context.Context, workers ...Worker) {
	if err := Start(ctx, workers...); err != nil {
		logger.Fatal(err)
	}
}

func Start(ctx context.Context, workers ...Worker) error {
	if len(workers) < 1 {
		return errors.New("need at least 1 worker")
	}

	ctx, cancel := signal.NotifyContext(ctx, os.Interrupt, os.Kill, syscall.SIGTERM)
	defer cancel()

	var errC = make(chan error, len(workers)*2)

	for _, worker := range workers {
		if worker == nil {
			return errors.New("received a nil worker")
		}

		go func(worker Worker) {
			if err := worker.Run(ctx); err != nil {
				errC <- err
				cancel()
			}
		}(worker)
	}

loop:
	for {
		select {
		case <-ctx.Done():
			var wg sync.WaitGroup
			wg.Add(len(workers))

			for _, worker := range workers {
				go func(worker Worker) {
					defer wg.Done()
					errC <- worker.Halt(ctx)
				}(worker)
			}

			wg.Wait()
			close(errC)

			break loop
		}
	}

	if err := NewMultiErrorFromChan(errC); err.Valid() {
		return err
	}

	return nil
}

type MultiError struct{ Errors []error }

func NewMultiErrorFromChan(errC chan error) MultiError {
	var errors []error
	for err := range errC {
		if err != nil {
			errors = append(errors, err)
		}
	}
	return MultiError{Errors: errors}
}

func (e MultiError) Valid() bool { return len(e.Errors) > 0 }

func (e MultiError) Error() string {
	switch len(e.Errors) {
	case 0:
		return "there are no errors"
	case 1:
		return e.Errors[0].Error()
	default:
		return fmt.Sprintf("there are more than one errors, first error: %v", e.Errors[0].Error())
	}
}
