package apperr

import (
	"errors"
	"fmt"
	"testing"
)

func TestError_CodeAndMessage(t *testing.T) {
	e := New("NOT_FOUND", "not found")
	if e.Code() != "NOT_FOUND" {
		t.Errorf("Code() = %q, want NOT_FOUND", e.Code())
	}
	if e.Error() != "not found" {
		t.Errorf("Error() = %q, want 'not found'", e.Error())
	}
}

func TestCode_ExtractsThroughWrapping(t *testing.T) {
	sentinel := New("INVALID_QUERY", "invalid query")
	wrapped := fmt.Errorf("%w: %v", sentinel, errors.New("query is empty"))

	code, ok := Code(wrapped)
	if !ok {
		t.Fatal("Code() ok = false, want true through a wrapped sentinel")
	}
	if code != "INVALID_QUERY" {
		t.Errorf("code = %q, want INVALID_QUERY", code)
	}

	// errors.Is must still match the sentinel after wrapping — use cases rely on
	// this in their tests.
	if !errors.Is(wrapped, sentinel) {
		t.Error("errors.Is lost the wrapped sentinel")
	}
}

func TestCode_AbsentReturnsFalse(t *testing.T) {
	if code, ok := Code(errors.New("plain")); ok {
		t.Errorf("Code() ok = true (code %q), want false for a non-coded error", code)
	}
}

func TestWrap_CarriesCauseAndPreservesIdentity(t *testing.T) {
	sentinel := New("UPSTREAM_UNAVAILABLE", "upstream unavailable")
	cause := errors.New("connection refused")
	wrapped := sentinel.Wrap(cause)

	// Identity: errors.Is still matches the sentinel.
	if !errors.Is(wrapped, sentinel) {
		t.Error("errors.Is(wrapped, sentinel) = false, want true")
	}
	// The cause stays reachable structurally and via errors.Is.
	if wrapped.Cause() != cause {
		t.Errorf("Cause() = %v, want %v", wrapped.Cause(), cause)
	}
	if !errors.Is(wrapped, cause) {
		t.Error("errors.Is(wrapped, cause) = false, want true")
	}
	// Code and message stay distinct (no stutter); Error() renders the chain.
	if wrapped.Code() != "UPSTREAM_UNAVAILABLE" || wrapped.Message() != "upstream unavailable" {
		t.Errorf("code/message = %q/%q", wrapped.Code(), wrapped.Message())
	}
	if got, want := wrapped.Error(), "upstream unavailable: connection refused"; got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
	// As recovers the structured error from a further-wrapped chain.
	if e := As(fmt.Errorf("outer: %w", wrapped)); e == nil || e.Code() != "UPSTREAM_UNAVAILABLE" {
		t.Errorf("As() failed to recover the coded error: %v", e)
	}
}
