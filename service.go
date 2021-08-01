package flex

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
)

var logger = log.New(os.Stderr, "flex: ", 0)

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
			var wg sync.WaitGroup
			wg.Add(len(workers))

			for _, worker := range workers {
				go func(worker Worker) {
					defer wg.Done()
					err := worker.Halt(ctx)
					haltErrC <- err
				}(worker)
			}

			wg.Wait()

			break loop
		}
	}

	close(errC)

	if err := newMultiErrorFromChan(errC); err.Valid() {
		return err
	}

	return nil
}

// MultiError holds a slice of errors and implements the error interface.
type MultiError struct{ Errors []error }

// newMultiErrorFromChan creates a new MultiError from a channel of errors.
func newMultiErrorFromChan(errC chan error) MultiError {
	var errors []error
	for err := range errC {
		if err != nil {
			errors = append(errors, err)
		}
	}
	return MultiError{Errors: errors}
}

// Valid returns true if the MultiError Errors slice is not empty.
func (e MultiError) Valid() bool { return len(e.Errors) > 0 }

// Error returns a string representation of the MultiError.
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

// Unwrap returns an error from Error (or nil if there are no errors).
// This error returned will further support Unwrap to get the next error,
// etc. The order will match the order of Errors in the multierror.Error
// at the time of calling.
func (e MultiError) Unwrap() error {
	// no errors, move along.
	if len(e.Errors) == 0 {
		return nil
	}

	// 1 error, return it directly.
	if len(e.Errors) == 1 {
		return e.Errors[0]
	}

	// many errors, return a formatted chain.
	var errChain []string
	for _, err := range e.Errors {
		errChain = append(errChain, err.Error())
	}

	return fmt.Errorf("multiple errors: %v", strings.Join(errChain, "; next => "))
}
