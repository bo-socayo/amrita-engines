// Package engines provides deterministic state machine engines for Temporal workflows.
//
// These engines are purely functional state machines designed to run in Temporal
// workflows without any non-deterministic behavior like time, random, or I/O operations.
//
// Architecture:
//   - Engine[TState, TEvent, TTransitionInfo]: Core engine interface
//   - BaseEngine[TState, TEvent, TTransitionInfo]: Concrete implementation with common functionality
//   - EventEnvelope[TEvent]: Type-safe wrapper for events with metadata
//   - Direct state and transition info returns for idiomatic Go
//
// The engines support both single event processing and batch processing with
// two-phase commit (2PC) for transactional guarantees across multiple engines.
package engines

import (
	"context"
	"fmt"
	"reflect"

	basev1 "github.com/bo-socayo/amrita-engines/gen/engines/base/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Type aliases for generated protobuf types
type (
	TransitionError       = basev1.TransitionError
	ProcessingStatus      = basev1.ProcessingStatus
	PrepareResult         = basev1.PrepareResult
	PrepareStatus         = basev1.PrepareStatus
	ErrorCode             = basev1.ErrorCode
	BaseEngineState       = basev1.BaseEngineState
	EngineStatus          = basev1.EngineStatus
	TransactionState      = basev1.TransactionState
)

// Custom Go error types for idiomatic error handling
// These replace the non-idiomatic response wrapper pattern

// EngineError is the base error type for all engine errors
type EngineError struct {
	Code    ErrorCode
	Message string
	Cause   error
}

func (e *EngineError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %v", e.Message, e.Cause)
	}
	return e.Message
}

func (e *EngineError) Unwrap() error {
	return e.Cause
}

// ValidationError represents state or event validation failures
type ValidationError struct {
	*EngineError
	Field string
}

// BusinessRuleViolationError represents business logic constraint violations
type BusinessRuleViolationError struct {
	*EngineError
	Rule string
}

// AlreadyInitializedError is returned when trying to initialize an already initialized engine
type AlreadyInitializedError struct {
	*EngineError
}

// NotInitializedError is returned when trying to use an uninitialized engine
type NotInitializedError struct {
	*EngineError
}

// TransactionError represents transaction-related failures
type TransactionError struct {
	*EngineError
	TransactionID int64
}

// Error constructors for idiomatic usage

func NewValidationError(field, message string) error {
	return &ValidationError{
		EngineError: &EngineError{
			Code:    basev1.ErrorCode_ERROR_CODE_INVALID_STATE,
			Message: fmt.Sprintf("validation failed for field %s: %s", field, message),
		},
		Field: field,
	}
}

func NewAlreadyInitializedError() error {
	return &AlreadyInitializedError{
		EngineError: &EngineError{
			Code:    basev1.ErrorCode_ERROR_CODE_ALREADY_INITIALIZED,
			Message: "engine already initialized",
		},
	}
}

func NewNotInitializedError() error {
	return &NotInitializedError{
		EngineError: &EngineError{
			Code:    basev1.ErrorCode_ERROR_CODE_INITIALIZATION_FAILED,
			Message: "engine not initialized",
		},
	}
}

func NewBusinessRuleViolationError(rule, message string) error {
	return &BusinessRuleViolationError{
		EngineError: &EngineError{
			Code:    basev1.ErrorCode_ERROR_CODE_BUSINESS_RULE_VIOLATION,
			Message: fmt.Sprintf("business rule violation (%s): %s", rule, message),
		},
		Rule: rule,
	}
}

func NewTransactionError(transactionID int64, message string) error {
	return &TransactionError{
		EngineError: &EngineError{
			Code:    basev1.ErrorCode_ERROR_CODE_TRANSACTION_CONFLICT,
			Message: fmt.Sprintf("transaction %d: %s", transactionID, message),
		},
		TransactionID: transactionID,
	}
}

// newInstance creates a new instance of type T for unmarshaling
// Handles both value types and pointer types properly
func newInstance[T any]() T {
	var zero T
	t := reflect.TypeOf(zero)
	
	// If T is a pointer type, create a new instance of the pointed-to type
	if t != nil && t.Kind() == reflect.Ptr {
		// Create new instance of the element type
		elem := reflect.New(t.Elem())
		return elem.Interface().(T)
	}
	
	// For non-pointer types, return zero value
	return zero
}

// isNilOrZero checks if a generic value is nil or zero
func isNilOrZero[T any](value T) bool {
	v := reflect.ValueOf(value)
	if !v.IsValid() {
		return true
	}
	switch v.Kind() {
	case reflect.Ptr, reflect.Interface, reflect.Slice, reflect.Map, reflect.Chan, reflect.Func:
		return v.IsNil()
	default:
		return v.IsZero()
	}
}

// TypedEventEnvelope provides type-safe access to EventEnvelope data
type TypedEventEnvelope[TEvent proto.Message] struct {
	*EventEnvelope
	eventData TEvent // cached typed data
}

// NewTypedEventEnvelope creates a new typed event envelope
func NewTypedEventEnvelope[TEvent proto.Message](
	sequenceNumber int64,
	eventID string,
	timestamp *timestamppb.Timestamp,
	data TEvent,
	metadata proto.Message,
) (*TypedEventEnvelope[TEvent], error) {
	envelope, err := NewEventEnvelope(sequenceNumber, eventID, timestamp, data, metadata)
	if err != nil {
		return nil, err
	}
	
	return &TypedEventEnvelope[TEvent]{
		EventEnvelope: envelope,
		eventData:     data,
	}, nil
}

// GetEventData returns the typed event data
func (e *TypedEventEnvelope[TEvent]) GetEventData() TEvent {
	return e.eventData
}

// UnmarshalEventData extracts and returns the typed event data
func (e *TypedEventEnvelope[TEvent]) UnmarshalEventData() (TEvent, error) {
	var zero TEvent
	if e.Data == nil {
		return zero, fmt.Errorf("envelope data is nil")
	}
	
	// Create a new instance of the type for unmarshaling
	target := newInstance[TEvent]()
	err := e.Data.UnmarshalTo(target)
	if err != nil {
		return zero, err
	}
	
	e.eventData = target
	return target, nil
}

// Validate validates the typed event envelope using protovalidate
func (e *TypedEventEnvelope[TEvent]) Validate() error {
	return ValidateProtoMessage(e.EventEnvelope, "EventEnvelope")
}

// REMOVED: TypedStateChangeResponse for idiomatic Go design

// REMOVED: All typed response wrapper types for idiomatic Go design
// These are replaced by direct state returns with proper error handling

// Engine defines the core interface that all engines must implement.
// It uses Go generics for type safety while maintaining deterministic behavior for Temporal.
// The protobuf types are used for serialization, validation, and wire format.
//
// Type Parameters:
//   - TState: The state type this engine manages (must be a protobuf message)
//   - TEvent: The event type this engine processes (must be a protobuf message)
//   - TTransitionInfo: Information about state transitions (must be a protobuf message)
//
// The interface supports both single event processing and batch processing with 2PC.
// IDIOMATIC GO: Methods return (result, error) instead of (response, error) with success flags.
type Engine[TState, TEvent, TTransitionInfo proto.Message] interface {
	// SetInitialState initializes the engine with an initial state.
	// This must be called before processing any events.
	//
	// Args:
	//   ctx: Context for cancellation (should be deterministic in Temporal)
	//   initialState: The initial state to set
	//   initializedAt: Timestamp when initialization occurred (for Temporal determinism)
	//
	// Returns:
	//   (finalState, error) - finalState is the state after applying defaults
	//   Error types: AlreadyInitializedError, ValidationError
	SetInitialState(ctx context.Context, initialState TState, initializedAt *timestamppb.Timestamp) (TState, error)

	// ProcessEvent is a convenience method for processing a single event.
	// This wraps the prepare/commit flow for simple non-transactional use cases.
	//
	// Args:
	//   ctx: Context for cancellation
	//   envelope: The event envelope to process
	//
	// Returns:
	//   (newState, transitionInfo, error) - idiomatic multiple return pattern
	//   Error types: NotInitializedError, ValidationError, BusinessRuleViolationError
	ProcessEvent(ctx context.Context, envelope *TypedEventEnvelope[TEvent]) (TState, TTransitionInfo, error)

	// ProcessEvents processes a batch of events atomically.
	// This is also a convenience wrapper around prepare/commit.
	//
	// Args:
	//   ctx: Context for cancellation
	//   envelopes: List of event envelopes to process
	//
	// Returns:
	//   (newState, transitionInfos, error) - batch processing results
	//   Error types: NotInitializedError, ValidationError, BusinessRuleViolationError
	ProcessEvents(ctx context.Context, envelopes []*TypedEventEnvelope[TEvent]) (TState, []TTransitionInfo, error)

	// GetCurrentState returns the current state of the engine.
	// Error types: NotInitializedError
	GetCurrentState() (TState, error)

	// GetLastProcessedSequence returns the sequence number of the last processed event.
	GetLastProcessedSequence() int64

	// IsInitialized returns true if SetInitialState has been called successfully.
	IsInitialized() bool

	// GetEngineName returns a string identifier for this engine.
	GetEngineName() string

	// CompressState allows engines to optimize/compress state before continue-as-new.
	// This hook is called by the entity workflow before continuing as new, allowing
	// engines to remove unnecessary data, compress collections, or archive old state.
	//
	// Args:
	//   ctx: Context for cancellation
	//   currentState: The current state to potentially compress
	//
	// Returns:
	//   (compressedState, error) - optimized state for the new workflow
	//   The default implementation should just return currentState unchanged.
	//   Error types: ValidationError (if compression fails)
	CompressState(ctx context.Context, currentState TState) (TState, error)

	// Two-Phase Commit Protocol
	// These methods support batch processing across multiple engines with transactional guarantees.

	// Prepare processes events and prepares for commit.
	// This is the first phase of 2PC - it processes events and generates a projected state
	// but doesn't commit the changes yet.
	//
	// Args:
	//   ctx: Context for cancellation
	//   envelopes: Events to process and prepare
	//
	// Returns:
	//   (prepareResult, projectedState, error) - preparation results
	//   Error types: NotInitializedError, ValidationError, TransactionError
	Prepare(ctx context.Context, envelopes []*TypedEventEnvelope[TEvent]) (PrepareResult, TState, error)

	// Commit applies the prepared transaction permanently.
	// This is the second phase of 2PC - it makes the prepared changes permanent.
	//
	// Args:
	//   ctx: Context for cancellation
	//   transactionID: Transaction ID from Prepare
	//
	// Returns:
	//   (finalState, error) - committed state
	//   Error types: TransactionError
	Commit(ctx context.Context, transactionID int64) (TState, error)

	// Abort discards the prepared transaction and restores the previous state.
	// This is used when 2PC coordination fails.
	//
	// Args:
	//   ctx: Context for cancellation
	//   transactionID: Transaction ID from Prepare
	//
	// Returns:
	//   (restoredState, error) - state after abort
	//   Error types: TransactionError
	Abort(ctx context.Context, transactionID int64) (TState, error)

	// GetTransactionState returns the current transaction state.
	GetTransactionState() TransactionState

	// GetCurrentTransactionID returns the ID of the currently active transaction, if any.
	GetCurrentTransactionID() *int64
}

// ProcessorFunc defines the signature for the core business logic function.
// Concrete engines must implement this to define their event processing logic.
//
// This function must be PURE - no side effects, deterministic, and idempotent.
// Perfect for Temporal workflows.
//
// The function should focus purely on business logic and return:
// - newState: The updated state after processing the event
// - transitionInfo: Information about what changed
// - error: Any business logic error that occurred
//
// The BaseEngine handles creating the StateChangeResponse.
//
// Args:
//   ctx: Context for cancellation
//   envelope: Event to process
//   currentState: Current engine state
//
// Returns:
//   newState: Updated state after processing
//   transitionInfo: Details about the state transition
//   error: Business logic error, if any
type ProcessorFunc[TState, TEvent, TTransitionInfo proto.Message] func(
	ctx context.Context,
	envelope *basev1.EventEnvelope,
	currentState TState,
) (newState TState, transitionInfo TTransitionInfo, err error)

// DefaultsFunc defines the signature for applying business defaults to state.
// This is called after state initialization and updates to ensure consistency.
//
// Args:
//   state: State to apply defaults to
//
// Returns:
//   State with business defaults applied
type DefaultsFunc[TState proto.Message] func(state TState) TState 