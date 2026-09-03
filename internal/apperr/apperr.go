// Package apperr is the project's error system. Every fallible function in the
// service and repository layers returns an error that is ALWAYS an *AppError
// drawn from the ERRORS catalog below — never a bare errors.New or fmt.Errorf,
// and never an inline &AppError{}. The HTTP error middleware unwraps it with
// From() to produce the JSON envelope and status code.
package apperr

import (
	"errors"
	"fmt"
)

// AppError is a domain error carrying a stable numeric code and an HTTP status.
type AppError struct {
	// Message is safe to return to the client.
	Message string
	// Code is a stable identifier — see the numbering convention below.
	Code int
	// HTTPStatus is the response status the middleware will send.
	HTTPStatus int
	// wrapped is an optional underlying cause, kept for logs only (never serialised).
	wrapped error
}

func (e *AppError) Error() string {
	if e.wrapped != nil {
		return fmt.Sprintf("%s (code %d): %v", e.Message, e.Code, e.wrapped)
	}
	return fmt.Sprintf("%s (code %d)", e.Message, e.Code)
}

func (e *AppError) Unwrap() error { return e.wrapped }

// Wrap attaches an underlying cause for logging without changing the client-facing
// message/code. Use it at the boundary where a driver/library error is converted:
//
//	if err != nil { return apperr.ERRORS.DatabaseError.Wrap(err) }
func (e *AppError) Wrap(cause error) *AppError {
	c := *e
	c.wrapped = cause
	return &c
}

// From normalises any error into an *AppError. Unknown errors become UnhandledError.
func From(err error) *AppError {
	if err == nil {
		return nil
	}
	var ae *AppError
	if errors.As(err, &ae) {
		return ae
	}
	return ERRORS.UnhandledError.Wrap(err)
}

func def(msg string, code, status int) *AppError {
	return &AppError{Message: msg, Code: code, HTTPStatus: status}
}

// Numbering convention:
//
//	1xxxx  common / general
//	2xxxx  authentication & authorization
//	3xxxx  users
//	4xxxx  screenings
//	5xxxx  files / storage
//	6xxxx  external screening engine
//	7xxxx  blacklist
//	8xxxx  checkpoints
//
// Add every new error here as a named field — reference it, never build one inline.
var ERRORS = struct {
	// common (1xxxx)
	DatabaseError      *AppError
	InvalidRequestBody *AppError
	InvalidQueryParam  *AppError
	InvalidPathParam   *AppError
	ValidationError    *AppError
	ResourceNotFound   *AppError
	DuplicateResource  *AppError
	UnhandledError     *AppError
	RouteNotFound      *AppError
	TooManyRequests    *AppError

	// auth (2xxxx)
	NoTokenProvided     *AppError
	InvalidAuthToken    *AppError
	TokenExpired        *AppError
	InvalidRefreshToken *AppError
	Unauthorized        *AppError
	Forbidden           *AppError
	InvalidCredentials  *AppError
	UserDisabled        *AppError

	// users (3xxxx)
	UserNotFound           *AppError
	UsernameTaken          *AppError
	EmailTaken             *AppError
	InvalidRole            *AppError
	InvalidCurrentPassword *AppError
	MissingScopeField      *AppError

	// screenings (4xxxx)
	ScreeningNotFound     *AppError
	AlreadyDecided        *AppError
	InvalidDocType        *AppError
	ScreeningNotCompleted *AppError
	InvalidDecision       *AppError

	// files / storage (5xxxx)
	FileRequired    *AppError
	FileTooLarge    *AppError
	InvalidFileType *AppError
	StorageFailed   *AppError

	// external screening engine (6xxxx)
	ScreeningEngineUnavailable *AppError
	ScreeningEngineBadResponse *AppError

	// blacklist (7xxxx)
	BlacklistEntryNotFound *AppError
	BlacklistEntryExists   *AppError
	InvalidBlacklistKind   *AppError
	BlacklistFieldsMissing *AppError

	// checkpoints (8xxxx)
	CheckpointNotFound      *AppError
	CheckpointExists        *AppError
	InvalidCheckpointStatus *AppError
	UnknownRegion           *AppError
}{
	DatabaseError:      def("Database operation failed", 10001, 500),
	InvalidRequestBody: def("Invalid request body", 10002, 400),
	InvalidQueryParam:  def("Invalid query parameters", 10003, 400),
	InvalidPathParam:   def("Invalid path parameter", 10004, 400),
	ValidationError:    def("Validation failed", 10005, 422),
	ResourceNotFound:   def("Resource not found", 10006, 404),
	DuplicateResource:  def("Resource already exists", 10007, 409),
	UnhandledError:     def("An unexpected error occurred", 10008, 500),
	RouteNotFound:      def("Route not found", 10009, 404),
	TooManyRequests:    def("Too many requests, please slow down", 10011, 429),

	NoTokenProvided:     def("No authentication token provided", 20001, 401),
	InvalidAuthToken:    def("Invalid authentication token", 20002, 401),
	TokenExpired:        def("Authentication token has expired", 20003, 401),
	InvalidRefreshToken: def("Invalid refresh token", 20004, 401),
	Unauthorized:        def("Unauthorized", 20005, 401),
	Forbidden:           def("Access forbidden", 20006, 403),
	InvalidCredentials:  def("Invalid username or password", 20007, 401),
	UserDisabled:        def("This account is disabled", 20008, 403),

	UserNotFound:           def("User not found", 30001, 404),
	UsernameTaken:          def("That username is already taken", 30002, 409),
	EmailTaken:             def("That email is already registered", 30003, 409),
	InvalidRole:            def("Unknown role", 30004, 422),
	InvalidCurrentPassword: def("Current password is incorrect", 30005, 401),
	MissingScopeField:      def("This role requires a region (admin) or checkpoint (verifier)", 30006, 422),

	ScreeningNotFound:     def("Screening not found", 40001, 404),
	AlreadyDecided:        def("This screening already has an officer decision", 40002, 409),
	InvalidDocType:        def("Unknown document type", 40003, 422),
	ScreeningNotCompleted: def("Screening has not finished processing", 40004, 409),
	InvalidDecision:       def("Decision must be one of accept, escalate, reject", 40005, 422),

	FileRequired:    def("A document image file is required", 50001, 400),
	FileTooLarge:    def("Uploaded file is too large", 50002, 413),
	InvalidFileType: def("Only JPEG or PNG images are accepted", 50003, 422),
	StorageFailed:   def("Failed to store the uploaded file", 50004, 500),

	ScreeningEngineUnavailable: def("The screening service is unavailable", 60001, 502),
	ScreeningEngineBadResponse: def("The screening service returned an unexpected response", 60002, 502),

	BlacklistEntryNotFound: def("Blacklist entry not found", 70001, 404),
	BlacklistEntryExists:   def("An active blacklist entry already matches this", 70002, 409),
	InvalidBlacklistKind:   def("Blacklist kind must be one of document, identity", 70003, 422),
	BlacklistFieldsMissing: def("Missing required fields for this blacklist kind", 70004, 422),

	CheckpointNotFound:      def("Checkpoint not found", 80001, 404),
	CheckpointExists:        def("A checkpoint with that code already exists", 80002, 409),
	InvalidCheckpointStatus: def("Checkpoint status must be one of active, attention", 80003, 422),
	UnknownRegion:           def("No checkpoint is registered in that region", 80004, 422),
}
