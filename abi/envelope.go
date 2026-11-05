// Package abi implements the Go side of the CPA native plugin contract.
//
// A CPA plugin is a native dynamic library that exports cliproxy_plugin_init
// together with a single call/free/shutdown function table. Every exchange
// between host and plugin is a JSON envelope:
//
//	{"ok":true,"result":{...}}
//	{"ok":false,"error":{"code":"...","message":"..."}}
//
// This package owns everything the official sdk/pluginapi and sdk/pluginabi
// leave unimplemented: capability registration, method dispatch, the typed host
// callbacks (host.http.do, host.http.* stream operations, host.auth.*,
// host.log) and the goroutine-safe per-invocation state a plugin needs.
//
// The wire types themselves come from sdk/pluginabi, which defines Envelope and
// Error but no constructor that carries an HTTP status, a wrapped sentinel, or
// a retry flag. Those three additions live here as a thin layer on top, so a
// plugin error can reach the CPA client with the status the upstream actually
// returned (see HTTPError) and errors.Is keeps matching a sentinel that had to
// be rewritten to carry one (see WithCause).
//
// The cgo glue that exports the four C symbols lives in each plugin's main
// package because //export is only honoured in package main.
package abi

import (
	"encoding/json"
	"fmt"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginabi"
)

// Envelope is the framing used for every host <-> plugin exchange.
type Envelope = pluginabi.Envelope

// EnvelopeError mirrors pluginabi.Error and adds the three things the official
// type cannot express: a wrapped sentinel (for errors.Is), and constructors
// that attach an HTTP status or a retry flag.
//
// The fields are duplicated rather than embedded because pluginabi.Error is a
// plain struct with no Unwrap, and an error that travels to the host must
// serialise with the exact same JSON tags.
type EnvelopeError struct {
	Code       string `json:"code"`
	Message    string `json:"message"`
	Retryable  bool   `json:"retryable,omitempty"`
	HTTPStatus int    `json:"http_status,omitempty"`
	// cause is the sentinel this error was built from, if any. It is unexported
	// and untagged so it never reaches the wire, and Unwrap exposes it so
	// errors.Is keeps working after a sentinel is wrapped to attach a status.
	cause error
}

func (e *EnvelopeError) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

// Unwrap exposes the wrapped sentinel so errors.Is(err, ErrQueueFull) keeps
// working on an error that was rewritten to carry an HTTP status.
func (e *EnvelopeError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

// WithCause attaches a sentinel to an envelope error so errors.Is keeps
// matching after the error was rewritten to carry an HTTP status.
func (e *EnvelopeError) WithCause(cause error) *EnvelopeError {
	if e != nil {
		e.cause = cause
	}
	return e
}

// StatusCode reports the HTTP status this error carries, or 0 when it carries
// none. It satisfies the same shape the host's clienterror package looks for,
// so an *EnvelopeError returned from an executor is classified correctly.
func (e *EnvelopeError) StatusCode() int {
	if e == nil {
		return 0
	}
	return e.HTTPStatus
}

// Errorf builds a plain (non-HTTP) plugin error.
func Errorf(code, format string, args ...any) *EnvelopeError {
	return &EnvelopeError{Code: code, Message: fmt.Sprintf(format, args...)}
}

// HTTPError builds an error that carries an HTTP status code through to the
// CPA client (used by executor methods).
func HTTPError(code string, status int, format string, args ...any) *EnvelopeError {
	return &EnvelopeError{Code: code, Message: fmt.Sprintf(format, args...), HTTPStatus: status}
}

// RetryableError marks a failure as retryable so the host may re-dispatch it.
func RetryableError(code string, format string, args ...any) *EnvelopeError {
	return &EnvelopeError{Code: code, Message: fmt.Sprintf(format, args...), Retryable: true}
}

// OK wraps a method result in a success envelope.
func OK(v any) ([]byte, error) {
	var raw json.RawMessage
	if v != nil {
		encoded, err := json.Marshal(v)
		if err != nil {
			return nil, fmt.Errorf("marshal plugin result: %w", err)
		}
		raw = encoded
	} else {
		raw = json.RawMessage("{}")
	}
	return json.Marshal(Envelope{OK: true, Result: raw})
}

// encodeError wraps an error in a failure envelope, preserving HTTP status and
// retryability when the error is an *EnvelopeError.
func encodeError(err error) []byte {
	envErr := &EnvelopeError{Code: "plugin_error", Message: err.Error()}
	if typed, ok := err.(*EnvelopeError); ok && typed != nil {
		envErr = typed
	}
	raw, marshalErr := json.Marshal(Envelope{OK: false, Error: envErr.toPluginABI()})
	if marshalErr != nil {
		// Last-resort literal; must never fail to produce a reply.
		return []byte(`{"ok":false,"error":{"code":"plugin_error","message":"plugin failed to encode error"}}`)
	}
	return raw
}

// toPluginABI projects this error onto the wire type. Envelope is an alias of
// pluginabi.Envelope, whose Error field is *pluginabi.Error, so the failure
// path has to convert rather than assign.
func (e *EnvelopeError) toPluginABI() *pluginabi.Error {
	if e == nil {
		return nil
	}
	return &pluginabi.Error{
		Code:       e.Code,
		Message:    e.Message,
		Retryable:  e.Retryable,
		HTTPStatus: e.HTTPStatus,
	}
}

// decodeEnvelope is used by the transport layer to inspect host replies.
func decodeEnvelope(raw []byte) (Envelope, error) {
	var env Envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return Envelope{}, fmt.Errorf("decode envelope: %w", err)
	}
	return env, nil
}
