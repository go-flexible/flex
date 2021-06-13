package flex_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/go-flexible/flex"
)

type mockWorker struct {
	name string
	t    *testing.T
}

func (m *mockWorker) Run(context.Context) error {
	if m.name == "" {
		m.name = "un-named"
	}
	m.t.Logf("mock worker (%s) running", m.name)
	return nil
}
func (m *mockWorker) Halt(context.Context) error {
	if m.name == "" {
		m.name = "un-named"
	}
	m.t.Logf("mock worker (%s) halting", m.name)
	return nil
}

type failingMockWorker struct{ mockWorker }

func (f *failingMockWorker) Run(context.Context) error {
	if f.name == "" {
		f.name = "un-named"
	}
	f.t.Logf("mock worker (%s) failing to run", f.name)
	return errors.New("run failed")
}

func defaultCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 2*time.Second)
}

func TestStart(t *testing.T) {
	t.Run("nil worker must not panic", func(t *testing.T) {
		t.Parallel()

		defer func() {
			if r := recover(); r != nil {
				t.Error("TestStart must not panic")
			}
		}()

		ctx, cancel := defaultCtx()
		defer cancel()

		err := flex.Start(ctx, nil)
		if err == nil {
			t.Error("expected an error but did not get one")
		}
	})
	t.Run("zero workers must return an error", func(t *testing.T) {
		t.Parallel()

		ctx, cancel := defaultCtx()
		defer cancel()

		err := flex.Start(ctx)
		if err == nil {
			t.Error("expected an error but did not get one")
		}
	})
	t.Run("one worker must run and halt successfully", func(t *testing.T) {
		t.Parallel()

		ctx, cancel := defaultCtx()
		defer cancel()

		err := flex.Start(ctx, &mockWorker{t: t, name: "foo"})
		if err != nil {
			t.Error(err)
		}
	})
	t.Run("multiple workers must run and halt successfully", func(t *testing.T) {
		t.Parallel()

		ctx, cancel := defaultCtx()
		defer cancel()

		workers := []flex.Worker{
			&mockWorker{t: t, name: "foo"},
			&mockWorker{t: t, name: "bar"},
			&mockWorker{t: t, name: "baz"},
		}

		err := flex.Start(ctx, workers...)
		if err != nil {
			t.Error(err)
		}
	})
	t.Run("one worker failing to run must cancel and error", func(t *testing.T) {
		t.Parallel()

		ctx, cancel := defaultCtx()
		defer cancel()

		workers := []flex.Worker{
			&failingMockWorker{mockWorker{t: t, name: "foo"}},
		}

		err := flex.Start(ctx, workers...)
		if err == nil {
			t.Error("expected an error but did not get one")
		}

		if _, ok := err.(flex.MultiError); !ok {
			t.Errorf("expected an error of type %T, but got: %T", flex.MultiError{}, err)
		}
	})
	t.Run("one of multiple workers failing to run must cancel", func(t *testing.T) {
		t.Parallel()

		ctx, cancel := defaultCtx()
		defer cancel()

		workers := []flex.Worker{
			&mockWorker{t: t, name: "foo"},
			&failingMockWorker{mockWorker{t: t, name: "bar"}},
			&mockWorker{t: t, name: "baz"},
		}

		err := flex.Start(ctx, workers...)
		if err == nil {
			t.Error("expected an error but did not get one")
		}

		if _, ok := err.(flex.MultiError); !ok {
			t.Errorf("expected an error of type %T, but got: %T", flex.MultiError{}, err)
		}
	})
}
