// Package apperr carries application errors that pair a stable, machine-readable
// Code (the tool contract's error code: INVALID_QUERY, NOT_FOUND, …) with a
// human message and an optional underlying cause.
//
// Code, message and cause are kept as distinct fields — never pre-concatenated —
// so callers render them without stutter (a structured {code, message} instead
// of "INVALID_QUERY: invalid query") and logs can emit them as a nested group
// that carries full diagnostic context.
//
// The domain decides the code and message (domain error logic); the entrypoint
// reads them back to render the wire response and structured logs. This is the
// one error vocabulary the use cases and the server share, and the only correct
// home for it: use cases may not import each other, and cmd/server cannot be
// imported at all.
package apperr

import "errors"

// Error is a coded application error.
type Error struct {
	code     string
	message  string
	cause    error
	sentinel *Error // the sentinel this was derived from via Wrap; nil for sentinels
}

// New creates a coded sentinel (no cause). Use it for package-level errors a use
// case returns and its tests match with errors.Is:
//
//	var ErrNotFound = apperr.New("NOT_FOUND", "not found")
func New(code, message string) *Error {
	return &Error{code: code, message: message}
}

// Wrap derives a contextual error from sentinel e: the same code and message,
// plus an underlying cause for diagnostics. errors.Is(result, e) still holds,
// and the cause stays reachable via errors.Is / Unwrap.
func (e *Error) Wrap(cause error) *Error {
	return &Error{code: e.code, message: e.message, cause: cause, sentinel: e}
}

// Code is the stable, machine-readable contract code.
func (e *Error) Code() string { return e.code }

// Message is the human-readable message, free of the code (no stutter).
func (e *Error) Message() string { return e.message }

// Cause is the underlying error, or nil.
func (e *Error) Cause() error { return e.cause }

// Error renders the full chain — for logs and %v, not for the wire.
func (e *Error) Error() string {
	if e.cause != nil {
		return e.message + ": " + e.cause.Error()
	}
	return e.message
}

func (e *Error) Unwrap() error { return e.cause }

func (e *Error) Is(target error) bool {
	return target == e || (e.sentinel != nil && target == e.sentinel)
}

// As returns the first *Error in err's chain, or nil if none is present (which
// the caller should treat as an internal error).
func As(err error) *Error {
	var e *Error
	if errors.As(err, &e) {
		return e
	}
	return nil
}

// Code extracts the contract code from anywhere in err's chain. It reports false
// when no coded error is present.
func Code(err error) (string, bool) {
	if e := As(err); e != nil {
		return e.code, true
	}
	return "", false
}
