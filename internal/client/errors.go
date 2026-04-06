package client

import (
	"errors"
	"fmt"
)

// APIError represents a structured error response from the Thunder Compute API.
// Maps to thundertypes.ErrorResponse: { code, error, message }.
type APIError struct {
	StatusCode int    `json:"code"`
	ErrorType  string `json:"error"`
	Message    string `json:"message"`
}

func (e *APIError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("thunder api: %d %s: %s", e.StatusCode, e.ErrorType, e.Message)
	}
	return fmt.Sprintf("thunder api: %d %s", e.StatusCode, e.ErrorType)
}

func (e *APIError) IsNotFound() bool {
	return e.StatusCode == 404
}

func (e *APIError) IsConflict() bool {
	return e.StatusCode == 409
}

// IsPermanent returns true for errors that will not resolve by retrying
// (auth failures, bad requests, not found, forbidden).
func (e *APIError) IsPermanent() bool {
	return e.StatusCode == 401 || e.StatusCode == 403 || e.StatusCode == 404
}

// IsNotFoundError checks if an error (or any wrapped error) is a Thunder API 404.
func IsNotFoundError(err error) bool {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.IsNotFound()
	}
	return false
}

// IsConflictError checks if an error (or any wrapped error) is a Thunder API 409.
func IsConflictError(err error) bool {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.IsConflict()
	}
	return false
}

// IsPermanentError checks if an error (or any wrapped error) is a non-retryable API error.
func IsPermanentError(err error) bool {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.IsPermanent()
	}
	return false
}
