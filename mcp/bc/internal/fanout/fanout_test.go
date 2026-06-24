package fanout

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestRun_PreservesOrder(t *testing.T) {
	in := []int{1, 2, 3, 4, 5}
	out, err := Run(context.Background(), in, func(_ context.Context, v int) (int, error) {
		return v * v, nil
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	want := []int{1, 4, 9, 16, 25}
	for i := range want {
		if out[i] != want[i] {
			t.Fatalf("out = %v, want %v (order must follow input despite concurrency)", out, want)
		}
	}
}

func TestRun_Empty(t *testing.T) {
	out, err := Run(context.Background(), []string{}, func(_ context.Context, s string) (string, error) {
		t.Fatalf("fn must not run for empty input (got %q)", s)
		return "", nil
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out == nil || len(out) != 0 {
		t.Fatalf("out = %v, want non-nil empty slice", out)
	}
}

// TestRun_RunsConcurrently proves the work overlaps: every worker must reach its
// barrier before any is released. If Run executed sequentially the first worker
// would block forever on release and the watcher would never see n starts, so
// the guarded timeout below would fire.
func TestRun_RunsConcurrently(t *testing.T) {
	const n = 4
	started := make(chan struct{}, n)
	release := make(chan struct{})

	go func() {
		for i := 0; i < n; i++ {
			<-started
		}
		close(release) // only reachable once all n are in flight at once
	}()

	done := make(chan struct{})
	go func() {
		_, _ = Run(context.Background(), make([]struct{}, n), func(_ context.Context, _ struct{}) (int, error) {
			started <- struct{}{}
			<-release
			return 0, nil
		})
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not execute its workers concurrently")
	}
}

func TestRun_FirstErrorFailsTheBatch(t *testing.T) {
	sentinel := errors.New("boom")
	out, err := Run(context.Background(), []int{1, 2, 3}, func(_ context.Context, v int) (int, error) {
		if v == 2 {
			return 0, sentinel
		}
		return v, nil
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("err = %v, want sentinel", err)
	}
	if out != nil {
		t.Errorf("out = %v, want nil on failure", out)
	}
}

// TestRun_PrefersRealErrorOverCancellation pins the firstCause rule: when one
// worker fails and its cancellation lands on a sibling, Run surfaces the real
// failure, not the sibling's context.Canceled. Deterministic: worker 0 blocks
// until the cancel from worker 1's failure wakes it.
func TestRun_PrefersRealErrorOverCancellation(t *testing.T) {
	sentinel := errors.New("real failure")
	_, err := Run(context.Background(), []int{0, 1}, func(ctx context.Context, v int) (int, error) {
		if v == 1 {
			return 0, sentinel
		}
		<-ctx.Done() // wait for the sibling's failure to cancel us
		return 0, ctx.Err()
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("err = %v, want the real failure, not the cancellation it triggered", err)
	}
}
