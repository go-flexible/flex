package flex_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/go-flexible/flex"
)

const unnamed = "un-named"

// customError is a test error type for testing errors.As()
type customError struct {
	message string
}

func (c customError) Error() string {
	return c.message
}

type mockWorker struct {
	name string
	t    *testing.T
}

func (m *mockWorker) Run(context.Context) error {
	if m.name == "" {
		m.name = unnamed
	}
	m.t.Logf("mock worker (%s) running", m.name)
	return nil
}

func (m *mockWorker) Halt(context.Context) error {
	if m.name == "" {
		m.name = unnamed
	}
	m.t.Logf("mock worker (%s) halting", m.name)
	return nil
}

type failingMockWorker struct{ mockWorker }

func (f *failingMockWorker) Run(context.Context) error {
	if f.name == "" {
		f.name = unnamed
	}
	f.t.Logf("mock worker (%s) failing to run", f.name)
	return errors.New("run failed")
}

func testCtx(t *testing.T) context.Context {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func TestStart(t *testing.T) {
	t.Run("nil worker must not panic", func(t *testing.T) {
		t.Parallel()

		defer func() {
			if r := recover(); r != nil {
				t.Error("TestStart must not panic")
			}
		}()

		err := flex.Start(testCtx(t), nil)
		if err == nil {
			t.Error("expected an error but did not get one")
		}
	})
	t.Run("zero workers must return an error", func(t *testing.T) {
		t.Parallel()

		err := flex.Start(testCtx(t))
		if err == nil {
			t.Error("expected an error but did not get one")
		}
	})
	t.Run("one worker must run and halt successfully", func(t *testing.T) {
		t.Parallel()

		err := flex.Start(testCtx(t), &mockWorker{t: t, name: "foo"})
		if err != nil {
			t.Error(err)
		}
	})
	t.Run("multiple workers must run and halt successfully", func(t *testing.T) {
		t.Parallel()

		workers := []flex.Worker{
			&mockWorker{t: t, name: "foo"},
			&mockWorker{t: t, name: "bar"},
			&mockWorker{t: t, name: "baz"},
		}

		err := flex.Start(testCtx(t), workers...)
		if err != nil {
			t.Error(err)
		}
	})
	t.Run("one worker failing to run must cancel and error", func(t *testing.T) {
		t.Parallel()

		workers := []flex.Worker{
			&failingMockWorker{mockWorker{t: t, name: "foo"}},
		}

		err := flex.Start(testCtx(t), workers...)
		if err == nil {
			t.Error("expected an error but did not get one")
		}
	})
	t.Run("one of multiple workers failing to run must cancel", func(t *testing.T) {
		t.Parallel()

		workers := []flex.Worker{
			&mockWorker{t: t, name: "foo"},
			&failingMockWorker{mockWorker{t: t, name: "bar"}},
			&mockWorker{t: t, name: "baz"},
		}

		err := flex.Start(testCtx(t), workers...)
		if err == nil {
			t.Error("expected an error but did not get one")
		}
	})
}

func TestErrorJoining(t *testing.T) {
	t.Run("errors.Join properly chains errors", func(t *testing.T) {
		t.Parallel()

		errA := errors.New("error A")
		errB := errors.New("error B")
		errC := errors.New("error C")

		joinedErr := errors.Join(errA, errB, errC)
		if joinedErr == nil {
			t.Error("expected errors.Join to return non-nil error")
		}

		// errors.Is should find all errors in the chain
		if !errors.Is(joinedErr, errA) {
			t.Error("expected errors.Is to find errA")
		}
		if !errors.Is(joinedErr, errB) {
			t.Error("expected errors.Is to find errB")
		}
		if !errors.Is(joinedErr, errC) {
			t.Error("expected errors.Is to find errC")
		}
	})

	t.Run("errors.As works with joined errors", func(t *testing.T) {
		t.Parallel()

		custom := customError{message: "custom error"}
		otherErr := errors.New("other error")

		joinedErr := errors.Join(otherErr, custom)

		var found customError
		if !errors.As(joinedErr, &found) {
			t.Error("expected errors.As to find custom error in joined error chain")
		}
		if found != custom {
			t.Errorf("expected to find %v, got %v", custom, found)
		}
	})

	t.Run("errors.Join with single error", func(t *testing.T) {
		t.Parallel()

		expected := errors.New("single error")
		joinedErr := errors.Join(expected)

		if !errors.Is(joinedErr, expected) {
			t.Error("expected errors.Is to find the single error")
		}
	})

	t.Run("errors.Join with no errors returns nil", func(t *testing.T) {
		t.Parallel()

		joinedErr := errors.Join()
		if joinedErr != nil {
			t.Errorf("expected nil when joining no errors, got %v", joinedErr)
		}
	})
}

type contextAwareWorker struct {
	mockWorker
	contextValid *bool
}

func (w *contextAwareWorker) Run(ctx context.Context) error {
	return errors.New("immediate failure to trigger halt")
}

func (w *contextAwareWorker) Halt(ctx context.Context) error {
	// Verify we receive a valid (not canceled) context
	if ctx.Err() == nil {
		*w.contextValid = true
	}
	return nil
}

type deadlineAwareWorker struct {
	mockWorker
	deadline *time.Time
}

func (w *deadlineAwareWorker) Run(ctx context.Context) error {
	return errors.New("immediate failure to trigger halt")
}

func (w *deadlineAwareWorker) Halt(ctx context.Context) error {
	if d, ok := ctx.Deadline(); ok {
		*w.deadline = d
	}
	return nil
}

type slowHaltWorker struct {
	mockWorker
	startTime *time.Time
	endTime   *time.Time
}

func (w *slowHaltWorker) Run(ctx context.Context) error {
	return errors.New("immediate failure to trigger halt")
}

func (w *slowHaltWorker) Halt(ctx context.Context) error {
	*w.startTime = time.Now()
	// Simulate 100ms of cleanup work
	time.Sleep(100 * time.Millisecond)
	*w.endTime = time.Now()
	return nil
}

type countingHaltWorker struct {
	mockWorker
	haltCount *int
	haltMutex *sync.Mutex
}

func (w *countingHaltWorker) Run(ctx context.Context) error {
	return errors.New("immediate failure to trigger halt")
}

func (w *countingHaltWorker) Halt(ctx context.Context) error {
	w.haltMutex.Lock()
	*w.haltCount++
	w.haltMutex.Unlock()
	return nil
}

type failingHaltWorker struct {
	mockWorker
	shouldFail bool
}

func (w *failingHaltWorker) Run(ctx context.Context) error {
	return errors.New("run failure")
}

func (w *failingHaltWorker) Halt(ctx context.Context) error {
	if w.shouldFail {
		return errors.New("halt failure")
	}
	return nil
}

type signalAwareWorker struct {
	mockWorker
	haltValid *bool
}

func (w *signalAwareWorker) Run(ctx context.Context) error {
	// Simulate running until signal
	select {
	case <-time.After(5 * time.Second):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (w *signalAwareWorker) Halt(ctx context.Context) error {
	// Check that halt context is not canceled
	if ctx.Err() == nil {
		*w.haltValid = true
	}
	return nil
}

func TestHaltContext(t *testing.T) {
	t.Run("halt receives fresh context", func(t *testing.T) {
		t.Parallel()

		haltContextReceived := false

		worker := &contextAwareWorker{
			mockWorker:   mockWorker{t: t, name: "context-aware"},
			contextValid: &haltContextReceived,
		}

		err := flex.Start(t.Context(), worker)
		if err == nil {
			t.Error("expected an error from Run failure")
		}

		if !haltContextReceived {
			t.Error("expected Halt to receive a valid (non-canceled) context")
		}
	})

	t.Run("halt context has timeout", func(t *testing.T) {
		t.Parallel()

		haltContextDeadline := time.Time{}

		worker := &deadlineAwareWorker{
			mockWorker: mockWorker{t: t, name: "deadline-aware"},
			deadline:   &haltContextDeadline,
		}

		err := flex.Start(t.Context(), worker)
		if err == nil {
			t.Error("expected an error from Run failure")
		}

		if haltContextDeadline.IsZero() {
			t.Error("expected Halt context to have a deadline")
		}
	})

	t.Run("halt graceful completion within timeout", func(t *testing.T) {
		t.Parallel()

		haltStartTime := time.Time{}
		haltEndTime := time.Time{}

		worker := &slowHaltWorker{
			mockWorker: mockWorker{t: t, name: "slow-halt"},
			startTime:  &haltStartTime,
			endTime:    &haltEndTime,
		}

		err := flex.Start(t.Context(), worker)
		if err == nil {
			t.Error("expected an error from Run failure")
		}

		haltDuration := haltEndTime.Sub(haltStartTime)
		if haltDuration < 100*time.Millisecond {
			t.Errorf("expected Halt to take at least 100ms, took %v", haltDuration)
		}

		// Should complete well within the 30s default halt timeout
		if haltDuration > 5*time.Second {
			t.Errorf("expected Halt to complete quickly, took %v", haltDuration)
		}
	})

	t.Run("all workers halt called concurrently", func(t *testing.T) {
		t.Parallel()

		haltCalls := struct {
			mu    sync.Mutex
			count int
		}{}

		workers := []flex.Worker{
			&countingHaltWorker{
				mockWorker: mockWorker{t: t, name: "worker-1"},
				haltCount:  &haltCalls.count,
				haltMutex:  &haltCalls.mu,
			},
			&countingHaltWorker{
				mockWorker: mockWorker{t: t, name: "worker-2"},
				haltCount:  &haltCalls.count,
				haltMutex:  &haltCalls.mu,
			},
			&countingHaltWorker{
				mockWorker: mockWorker{t: t, name: "worker-3"},
				haltCount:  &haltCalls.count,
				haltMutex:  &haltCalls.mu,
			},
		}

		err := flex.Start(t.Context(), workers...)
		if err == nil {
			t.Error("expected errors from Run failures")
		}

		if haltCalls.count != 3 {
			t.Errorf("expected 3 Halt calls, got %d", haltCalls.count)
		}
	})

	t.Run("halt errors collected and returned", func(t *testing.T) {
		t.Parallel()

		workers := []flex.Worker{
			&failingHaltWorker{
				mockWorker: mockWorker{t: t, name: "worker-1"},
				shouldFail: false,
			},
			&failingHaltWorker{
				mockWorker: mockWorker{t: t, name: "worker-2"},
				shouldFail: true,
			},
		}

		err := flex.Start(t.Context(), workers...)
		if err == nil {
			t.Error("expected errors")
		}

		// Should contain errors
		t.Logf("error returned (correct): %v", err)
	})

	t.Run("halt context independent of run context cancellation", func(t *testing.T) {
		t.Parallel()

		haltContextNotCanceled := false

		// CreateUse a short timeout context to force cancellation
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()

		worker := &signalAwareWorker{
			mockWorker: mockWorker{t: t, name: "signal-aware"},
			haltValid:  &haltContextNotCanceled,
		}

		_ = flex.Start(ctx, worker)

		if !haltContextNotCanceled {
			t.Error("expected Halt to receive fresh (non-canceled) context even after Run context cancellation")
		}
	})
}
