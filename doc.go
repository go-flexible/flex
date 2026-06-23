// Package flex provides a service lifecycle manager for Go applications.
//
// It manages the concurrent startup, running, and graceful shutdown of
// service workers. Workers are started concurrently and run until one
// of the following occurs:
//   - A worker returns an error from Run()
//   - A signal (SIGINT, SIGKILL, SIGTERM) is received
//   - The provided context is canceled
//
// When shutdown is triggered, all workers' Halt() methods are called
// concurrently with a fresh context that has DefaultHaltTimeout as its
// deadline, ensuring a graceful shutdown.
//
// Start a service:
//
//	flex.MustStart(ctx, worker1, worker2)
//
// Or with error handling:
//
//	if err := flex.Start(ctx, worker1, worker2); err != nil {
//	    log.Fatal(err)
//	}
//
// With options:
//
//	err := flex.StartOpts(ctx, []flex.Worker{w1, w2}, flex.WithoutSignals())
package flex
