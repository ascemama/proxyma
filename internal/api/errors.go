// Package api exposes Proxyma HTTP handlers and response contracts.
package api

import (
	"encoding/json"
	"net/http"

	"proxyma/internal/transcriptor"
)

type requestError struct {
	status  int
	payload ErrorResponse
}

func (e requestError) Error() string {
	return e.payload.Error
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, ErrorResponse{Error: message})
}

func validationError(message string) error {
	return requestError{status: http.StatusBadRequest, payload: ErrorResponse{Error: message}}
}

func notFoundError(message string) error {
	return requestError{status: http.StatusNotFound, payload: ErrorResponse{Error: message}}
}

func transcriptorRequestError(err *transcriptor.Error) error {
	return requestError{
		status: http.StatusBadRequest,
		payload: ErrorResponse{
			Error:     err.Error(),
			Code:      err.Code,
			Retryable: err.Retryable,
			Hint:      err.Hint,
		},
	}
}

func errorResponse(err error) (int, ErrorResponse) {
	if typed, ok := err.(requestError); ok {
		return typed.status, typed.payload
	}
	return http.StatusInternalServerError, ErrorResponse{Error: err.Error()}
}
