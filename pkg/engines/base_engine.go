package engines

import (
	"context"
	"fmt"
	"reflect"

	basev1 "github.com/user/engines/gen/engines/base/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// BaseEngine provides a concrete implementation of the Engine interface.
// It handles common functionality like state management, validation, transactions,
// and idempotency checking. Concrete engines embed this and provide their specific
// business logic through the ProcessorFunc and DefaultsFunc.
//
// This follows the composition pattern rather than inheritance, making it more
// idiomatic Go while maintaining the same functionality as the Python version.
//
// TEMPORAL-SAFE: This engine is designed for use in Temporal workflows and does
// not use Go mutexes or any other non-deterministic concurrency primitives.
// Temporal workflows are single-threaded and deterministic by design.
type BaseEngine[TState, TEvent, TTransitionInfo proto.Message] struct {
	// Configuration
	engineName           string
	businessLogicVersion string
	processor            ProcessorFunc[TState, TEvent, TTransitionInfo]
	defaults             DefaultsFunc[TState]
	
	// State management
	businessState        TState
	shadowState          TState              // Projected state after prepare
	baseEngineState      *BaseEngineState
	preparedEvents       []*TypedEventEnvelope[TEvent] // Events in current transaction
	
	// Type information for reflection
	stateTypeName        string
	eventTypeName        string
	transitionTypeName   string
}

// BaseEngineConfig holds configuration for creating a BaseEngine.
type BaseEngineConfig[TState, TEvent, TTransitionInfo proto.Message] struct {
	EngineName           string
	BusinessLogicVersion string
	Processor            ProcessorFunc[TState, TEvent, TTransitionInfo]
	Defaults             DefaultsFunc[TState]
	StateTypeName        string
	EventTypeName        string
	TransitionTypeName   string
}

// NewBaseEngine creates a new BaseEngine with the provided configuration.
func NewBaseEngine[TState, TEvent, TTransitionInfo proto.Message](
	config BaseEngineConfig[TState, TEvent, TTransitionInfo],
) *BaseEngine[TState, TEvent, TTransitionInfo] {
	engine := &BaseEngine[TState, TEvent, TTransitionInfo]{
		engineName:           config.EngineName,
		businessLogicVersion: config.BusinessLogicVersion,
		processor:            config.Processor,
		defaults:             config.Defaults,
		stateTypeName:        config.StateTypeName,
		eventTypeName:        config.EventTypeName,
		transitionTypeName:   config.TransitionTypeName,
		baseEngineState:      &BaseEngineState{
			Status:           basev1.EngineStatus_ENGINE_STATUS_INITIALIZING,
			TransactionState: basev1.TransactionState_TRANSACTION_IDLE,
		},
	}
	
	return engine
}

// SetInitialState initializes the engine with an initial state.
// IDIOMATIC GO: Returns (state, error) instead of (response, error)
func (e *BaseEngine[TState, TEvent, TTransitionInfo]) SetInitialState(
	ctx context.Context,
	initialState TState,
	initializedAt *timestamppb.Timestamp,
) (TState, error) {
	var zero TState
	
	if e.baseEngineState.Initialized {
		return zero, NewAlreadyInitializedError()
	}
	
	// Validate the initial state
	if err := e.validateState(initialState); err != nil {
		return zero, NewValidationError("initialState", err.Error())
	}
	
	// Apply business defaults
	stateWithDefaults := e.applyDefaults(initialState)
	
	// Set the state
	e.businessState = stateWithDefaults
	e.baseEngineState.Initialized = true
	e.baseEngineState.Status = basev1.EngineStatus_ENGINE_STATUS_READY
	if initializedAt != nil {
		e.baseEngineState.InitializedAt = initializedAt
	} else {
		e.baseEngineState.InitializedAt = timestamppb.Now()
	}
	
	return stateWithDefaults, nil
}

// ProcessEvent processes a single event (convenience wrapper).
// IDIOMATIC GO: Returns (state, transitionInfo, error) instead of response wrapper
func (e *BaseEngine[TState, TEvent, TTransitionInfo]) ProcessEvent(
	ctx context.Context,
	envelope *TypedEventEnvelope[TEvent],
) (TState, TTransitionInfo, error) {
	newState, transitionInfos, err := e.ProcessEvents(ctx, []*TypedEventEnvelope[TEvent]{envelope})
	if err != nil {
		var zero TState
		var zeroTransition TTransitionInfo
		return zero, zeroTransition, err
	}
	
	if len(transitionInfos) == 0 {
		var zero TState
		var zeroTransition TTransitionInfo
		return zero, zeroTransition, fmt.Errorf("no transition info returned from ProcessEvents")
	}
	
	return newState, transitionInfos[0], nil
}

// ProcessEvents processes a batch of events atomically.
// IDIOMATIC GO: Returns (state, transitionInfos, error) instead of response wrapper
func (e *BaseEngine[TState, TEvent, TTransitionInfo]) ProcessEvents(
	ctx context.Context,
	envelopes []*TypedEventEnvelope[TEvent],
) (TState, []TTransitionInfo, error) {
	var zero TState
	var zeroTransitions []TTransitionInfo
	
	if len(envelopes) == 0 {
		return zero, zeroTransitions, fmt.Errorf("no events to process")
	}
	
	if !e.baseEngineState.Initialized {
		return zero, zeroTransitions, NewNotInitializedError()
	}
	
	// Process events and collect transition info
	var transitionInfos []TTransitionInfo
	originalState := e.businessState
	currentState := originalState
	
	// Process each event through the business logic
	for _, envelope := range envelopes {
		// Call the processor function for business logic
		newState, transitionInfo, err := e.processor(ctx, envelope.EventEnvelope, currentState)
		if err != nil {
			// Business logic error - reset state
			e.businessState = originalState
			return zero, zeroTransitions, fmt.Errorf("business logic error: %w", err)
		}
		
		// Validate the new state
		if err := e.validateState(newState); err != nil {
			// State validation error - reset state
			e.businessState = originalState
			return zero, zeroTransitions, NewValidationError("newState", err.Error())
		}
		
		// Update current state and collect transition info
		currentState = newState
		transitionInfos = append(transitionInfos, transitionInfo)
		
		// Update sequence tracking
		e.baseEngineState.LastProcessedSequence = envelope.SequenceNumber
	}
	
	// Commit the final state
	e.businessState = currentState
	
	return currentState, transitionInfos, nil
}

// GetCurrentState returns the current business state.
func (e *BaseEngine[TState, TEvent, TTransitionInfo]) GetCurrentState() (TState, error) {
	var zero TState
	if !e.baseEngineState.Initialized {
		return zero, fmt.Errorf("engine not initialized")
	}
	
	return e.businessState, nil
}

// GetLastProcessedSequence returns the sequence number of the last processed event.
func (e *BaseEngine[TState, TEvent, TTransitionInfo]) GetLastProcessedSequence() int64 {
	return e.baseEngineState.LastProcessedSequence
}

// IsInitialized returns true if the engine has been initialized.
func (e *BaseEngine[TState, TEvent, TTransitionInfo]) IsInitialized() bool {
	return e.baseEngineState.Initialized
}

// GetEngineName returns the engine name.
func (e *BaseEngine[TState, TEvent, TTransitionInfo]) GetEngineName() string {
	return e.engineName
}

// GetTransactionState returns the current transaction state.
func (e *BaseEngine[TState, TEvent, TTransitionInfo]) GetTransactionState() TransactionState {
	return e.baseEngineState.TransactionState
}

// GetCurrentTransactionID returns the current transaction ID if any.
func (e *BaseEngine[TState, TEvent, TTransitionInfo]) GetCurrentTransactionID() *int64 {
	if e.baseEngineState.CurrentTransactionId == 0 {
		return nil
	}
	return &e.baseEngineState.CurrentTransactionId
}

// Prepare processes events and prepares for commit (2PC phase 1).
// IDIOMATIC GO: Returns (PrepareResult, projectedState, error) instead of response wrapper
func (e *BaseEngine[TState, TEvent, TTransitionInfo]) Prepare(
	ctx context.Context,
	envelopes []*TypedEventEnvelope[TEvent],
) (PrepareResult, TState, error) {
	var zero TState
	var zeroPrepareResult PrepareResult
	
	// Check preconditions
	if !e.baseEngineState.Initialized {
		return zeroPrepareResult, zero, NewNotInitializedError()
	}
	
	if e.baseEngineState.TransactionState != basev1.TransactionState_TRANSACTION_IDLE {
		return zeroPrepareResult, zero, NewTransactionError(0, "transaction already in progress")
	}
	
	// Validate all envelopes
	for i, envelope := range envelopes {
		if err := ValidateProtoMessage(envelope.EventEnvelope, "EventEnvelope"); err != nil {
			return zeroPrepareResult, zero, NewValidationError(fmt.Sprintf("envelope[%d]", i), err.Error())
		}
		
		// Validate the event data inside the envelope
		eventData := envelope.GetEventData()
		if err := ValidateEventData(eventData); err != nil {
			return zeroPrepareResult, zero, NewValidationError(fmt.Sprintf("eventData[%d]", i), err.Error())
		}
		
		// Check sequence ordering (idempotency)
		if envelope.SequenceNumber <= e.baseEngineState.LastProcessedSequence {
			return zeroPrepareResult, zero, fmt.Errorf("sequence %d already processed (last: %d)", 
				envelope.SequenceNumber, e.baseEngineState.LastProcessedSequence)
		}
	}
	
	// Generate transaction ID
	e.baseEngineState.TransactionCounter++
	transactionID := e.baseEngineState.TransactionCounter
	
	// Process events and create shadow state
	currentState := e.businessState
	projectedState := currentState
	
	for _, envelope := range envelopes {
		// Convert TypedEventEnvelope to basev1.EventEnvelope for processor
		baseEnvelope := &basev1.EventEnvelope{
			SequenceNumber: envelope.SequenceNumber,
			EventId:        envelope.EventId,
			Timestamp:      envelope.Timestamp,
			Data:           envelope.Data,
			Metadata:       envelope.Metadata,
		}
		
		newState, transitionInfo, err := e.processor(ctx, baseEnvelope, projectedState)
		if err != nil {
			return zeroPrepareResult, zero, fmt.Errorf("processor error: %w", err)
		}
		
		// Validate the new state
		if err := e.validateState(newState); err != nil {
			return zeroPrepareResult, zero, NewValidationError("newState", err.Error())
		}
		
		// Validate the transition info
		if err := ValidateTransitionInfo(transitionInfo); err != nil {
			return zeroPrepareResult, zero, NewValidationError("transitionInfo", err.Error())
		}
		
		projectedState = newState
		_ = transitionInfo // We store transition info but don't use it in prepare phase
	}
	
	// Store prepared state
	e.shadowState = projectedState
	e.preparedEvents = envelopes
	e.baseEngineState.TransactionState = basev1.TransactionState_TRANSACTION_PREPARED
	e.baseEngineState.CurrentTransactionId = transactionID
	
	// Create and return PrepareResult
	prepareResult := PrepareResult{
		Status:         basev1.PrepareStatus_PREPARE_STATUS_PREPARED,
		TransactionId:  transactionID,
		CanCommit:      true,
		ProjectedState: nil, // We return the state directly in the second return value
		PrepareInfo:    fmt.Sprintf("prepared %d events", len(envelopes)),
	}
	
	return prepareResult, projectedState, nil
}

// Commit applies the prepared transaction permanently (2PC phase 2).
// IDIOMATIC GO: Returns (finalState, error) instead of response wrapper
func (e *BaseEngine[TState, TEvent, TTransitionInfo]) Commit(
	ctx context.Context,
	transactionID int64,
) (TState, error) {
	var zero TState
	
	// Validate transaction
	if e.baseEngineState.TransactionState != basev1.TransactionState_TRANSACTION_PREPARED {
		return zero, NewTransactionError(transactionID, "no prepared transaction to commit")
	}
	
	if e.baseEngineState.CurrentTransactionId != transactionID {
		return zero, NewTransactionError(transactionID, "transaction ID mismatch")
	}
	
	// Apply the changes
	e.businessState = e.shadowState
	
	// Update sequence tracking
	if len(e.preparedEvents) > 0 {
		lastEvent := e.preparedEvents[len(e.preparedEvents)-1]
		e.baseEngineState.LastProcessedSequence = lastEvent.SequenceNumber
	}
	
	// Update statistics
	e.baseEngineState.TotalEventsProcessed += int64(len(e.preparedEvents))
	e.baseEngineState.LastActivityAt = timestamppb.Now()
	
	// Clean up transaction state
	e.baseEngineState.TransactionState = basev1.TransactionState_TRANSACTION_COMMITTED
	e.baseEngineState.CurrentTransactionId = 0
	e.preparedEvents = nil
	var zeroShadow TState
	e.shadowState = zeroShadow
	
	// Reset to idle after commit
	e.baseEngineState.TransactionState = basev1.TransactionState_TRANSACTION_IDLE
	
	return e.businessState, nil
}

// Abort discards the prepared transaction (2PC abort).
// IDIOMATIC GO: Returns (restoredState, error) instead of response wrapper
func (e *BaseEngine[TState, TEvent, TTransitionInfo]) Abort(
	ctx context.Context,
	transactionID int64,
) (TState, error) {
	var zero TState
	
	// Validate transaction
	if e.baseEngineState.TransactionState != basev1.TransactionState_TRANSACTION_PREPARED {
		return zero, NewTransactionError(transactionID, "no prepared transaction to abort")
	}
	
	if e.baseEngineState.CurrentTransactionId != transactionID {
		return zero, NewTransactionError(transactionID, "transaction ID mismatch")
	}
	
	// Restore state (business state is already the restored state)
	restoredState := e.businessState
	
	// Clean up transaction state
	e.baseEngineState.TransactionState = basev1.TransactionState_TRANSACTION_ABORTED
	e.baseEngineState.CurrentTransactionId = 0
	e.preparedEvents = nil
	var zeroShadow TState
	e.shadowState = zeroShadow
	
	// Reset to idle after abort
	e.baseEngineState.TransactionState = basev1.TransactionState_TRANSACTION_IDLE
	
	return restoredState, nil
}

// Helper methods

func (e *BaseEngine[TState, TEvent, TTransitionInfo]) validateState(state TState) error {
	// Basic nil check - handle typed nil pointers correctly
	val := reflect.ValueOf(state)
	if !val.IsValid() || (val.Kind() == reflect.Ptr && val.IsNil()) {
		return fmt.Errorf("state cannot be nil")
	}
	
	// Use protovalidate to validate according to buf.validate rules in proto files
	if err := ValidateState(state); err != nil {
		return err
	}
	
	return nil
}

func (e *BaseEngine[TState, TEvent, TTransitionInfo]) applyDefaults(state TState) TState {
	if e.defaults != nil {
		return e.defaults(state)
	}
	return state
} 