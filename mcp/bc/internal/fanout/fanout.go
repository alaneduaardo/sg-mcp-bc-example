// Package fanout runs independent, context-aware operations concurrently and
// joins them in order. It exists because two use cases share one shape — N
// independent network calls that should run in parallel, preserve input order
// and fail fast on the first real error — and that mechanic (goroutines, a wait
// group, context cancellation) belongs in one tested place, not copied per use
// case. It imports nothing internal.
package fanout

import (
	"context"
	"errors"
	"sync"
)

// Run calls fn once per input, concurrently, and returns the results in input
// order. The first call to fail cancels the context handed to the others, so a
// doomed batch stops early — but Run still waits for every goroutine to return
// before it does, so it never leaks one. On success err is nil and len(results)
// == len(inputs); on failure results is nil and err is the first real failure,
// preferring it over a context.Canceled that a sibling's failure caused (the
// caller sees the cause, not the symptom). An empty input returns an empty,
// non-nil slice and a nil error without starting any goroutine.
//
// Results are written to disjoint slice indices, so the workers share no mutable
// state and Run is free of data races by construction (verified under -race).
func Run[T, R any](ctx context.Context, inputs []T, fn func(context.Context, T) (R, error)) ([]R, error) {
	results := make([]R, len(inputs))
	if len(inputs) == 0 {
		return results, nil
	}
	errs := make([]error, len(inputs))

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var wg sync.WaitGroup
	for i, in := range inputs {
		wg.Add(1)
		go func(i int, in T) {
			defer wg.Done()
			r, err := fn(ctx, in)
			if err != nil {
				errs[i] = err
				cancel() // one failure makes the joined result unreliable; stop the siblings
				return
			}
			results[i] = r
		}(i, in)
	}
	wg.Wait()

	if err := firstCause(errs); err != nil {
		return nil, err
	}
	return results, nil
}

// firstCause returns the most informative error from a fan-out: the first
// non-cancellation error (the failure that actually fired) in preference to a
// context.Canceled that Run's own cancel propagated to the siblings.
func firstCause(errs []error) error {
	var fallback error
	for _, e := range errs {
		if e == nil {
			continue
		}
		if fallback == nil {
			fallback = e
		}
		if !errors.Is(e, context.Canceled) {
			return e
		}
	}
	return fallback
}
