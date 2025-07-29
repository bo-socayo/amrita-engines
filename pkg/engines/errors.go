package engines

import (
	"fmt"
)

// Note: Type aliases are defined in engine.go to avoid duplication

// TransitionErrorString returns a string representation of the error
func TransitionErrorString(e *TransitionError) string {
	if e.GetFieldPath() != "" {
		return fmt.Sprintf("[%s] %s (field: %s)", e.GetCode(), e.GetMessage(), e.GetFieldPath())
	}
	return fmt.Sprintf("[%s] %s", e.GetCode(), e.GetMessage())
}

// NewTransitionError creates a new TransitionError
func NewTransitionError(code ErrorCode, message string) *TransitionError {
	return &TransitionError{
		Code:    code,
		Message: message,
	}
}

// NewTransitionErrorWithField creates a new TransitionError with field information
func NewTransitionErrorWithField(code ErrorCode, message, fieldPath string) *TransitionError {
	return &TransitionError{
		Code:      code,
		Message:   message,
		FieldPath: fieldPath,
	}
}

// NewTransitionErrorWithDetails creates a new TransitionError with additional details
func NewTransitionErrorWithDetails(code ErrorCode, message, details string) *TransitionError {
	return &TransitionError{
		Code:    code,
		Message: message,
		Details: details,
	}
}

// REMOVED: Old response constructor helpers for non-idiomatic response wrappers
// These have been replaced with proper Go error types in engine.go

// REMOVED: All obsolete response constructor functions for non-idiomatic patterns
// These have been replaced with proper Go error types and constructors in engine.go 