package demo_engine

import (
	"context"
	"fmt"

	basev1 "github.com/bo-socayo/amrita-engines/gen/engines/base/v1"
	demov1 "github.com/bo-socayo/amrita-engines/gen/engines/demo/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
)

const (
	// Business logic version - update when changing business logic
	BusinessLogicVersion = "v1.0.0"
	EngineName          = "demo-engine"
)

// DemoEngine manages counter state and operations
type DemoEngine struct {
	// This will be embedded in a BaseEngine in the actual implementation
}

// ProcessSignal processes a demo engine signal and returns the new state
// This is the core business logic function that would be used with BaseEngine
func ProcessSignal(
	ctx context.Context,
	envelope *basev1.EventEnvelope,
	currentState *demov1.DemoEngineState,
) (*demov1.DemoEngineState, *demov1.DemoEngineTransitionInfo, error) {
	// Clone the current state to avoid mutations
	newState := proto.Clone(currentState).(*demov1.DemoEngineState)
	
	// Extract the signal from the envelope
	signal := &demov1.DemoEngineSignal{}
	if err := envelope.Data.UnmarshalTo(signal); err != nil {
		return nil, nil, fmt.Errorf("failed to unmarshal demo signal: %w", err)
	}

	// Track events count before processing
	eventsCountBefore := int64(len(newState.History))
	
	// Initialize transition info
	transitionInfo := &demov1.DemoEngineTransitionInfo{
		ValidationPassed:    true,
		EventsCountBefore:   eventsCountBefore,
		ValidationMessage:   "",
	}

	// Process the signal based on its type
	switch signalType := signal.Signal.(type) {
	case *demov1.DemoEngineSignal_Increment:
		if err := processIncrementSignal(newState, signalType.Increment, transitionInfo); err != nil {
			return nil, nil, fmt.Errorf("failed to process increment signal: %w", err)
		}

	case *demov1.DemoEngineSignal_Reset_:
		if err := processResetSignal(newState, signalType.Reset_, transitionInfo); err != nil {
			return nil, nil, fmt.Errorf("failed to process reset signal: %w", err)
		}

	case *demov1.DemoEngineSignal_UpdateConfig:
		if err := processUpdateConfigSignal(newState, signalType.UpdateConfig, transitionInfo); err != nil {
			return nil, nil, fmt.Errorf("failed to process update config signal: %w", err)
		}

	default:
		return nil, nil, fmt.Errorf("unknown signal type: %T", signalType)
	}

	// Update last updated timestamp from envelope
	newState.LastUpdated = envelope.Timestamp
	
	// Update transition info with final counts
	transitionInfo.EventsCountAfter = int64(len(newState.History))

	return newState, transitionInfo, nil
}

// processIncrementSignal handles incrementing the counter
func processIncrementSignal(state *demov1.DemoEngineState, signal *demov1.IncrementSignal, transitionInfo *demov1.DemoEngineTransitionInfo) error {
	// Validate increment amount
	if signal.Amount <= 0 {
		transitionInfo.ValidationPassed = false
		transitionInfo.ValidationMessage = "increment amount must be positive"
		return fmt.Errorf("invalid increment amount: %d", signal.Amount)
	}

	previousValue := state.CurrentValue
	newValue := state.CurrentValue + signal.Amount

	// Check max value constraint if configured
	if state.Config.MaxValue > 0 && newValue > state.Config.MaxValue {
		transitionInfo.ValidationPassed = false
		transitionInfo.ValidationMessage = fmt.Sprintf("increment would exceed max value %d", state.Config.MaxValue)
		return fmt.Errorf("increment would exceed max value %d (current: %d, increment: %d)", 
			state.Config.MaxValue, state.CurrentValue, signal.Amount)
	}

	// Check negative constraint if not allowed
	if !state.Config.AllowNegative && newValue < 0 {
		transitionInfo.ValidationPassed = false
		transitionInfo.ValidationMessage = "negative values not allowed"
		return fmt.Errorf("negative values not allowed")
	}

	// Create event record
	event := &demov1.CounterEvent{
		EventId:       state.NextEventId,
		Type:          demov1.CounterEventType_COUNTER_EVENT_TYPE_INCREMENT,
		ValueChange:   signal.Amount,
		PreviousValue: previousValue,
		NewValue:      newValue,
		Timestamp:     signal.Timestamp,
		Reason:        signal.Reason,
	}

	// Update state
	state.CurrentValue = newValue
	state.History = append(state.History, event)
	state.NextEventId++
	
	// Record transition info
	transitionInfo.EventTriggered = event

	return nil
}

// processResetSignal handles resetting the counter to zero
func processResetSignal(state *demov1.DemoEngineState, signal *demov1.ResetSignal, transitionInfo *demov1.DemoEngineTransitionInfo) error {
	previousValue := state.CurrentValue
	newValue := int64(0)

	// Check negative constraint if resetting to zero would violate it
	if !state.Config.AllowNegative && newValue < 0 {
		transitionInfo.ValidationPassed = false
		transitionInfo.ValidationMessage = "cannot reset to zero when negative values not allowed"
		return fmt.Errorf("cannot reset to zero when negative values not allowed")
	}

	// Create event record
	event := &demov1.CounterEvent{
		EventId:       state.NextEventId,
		Type:          demov1.CounterEventType_COUNTER_EVENT_TYPE_RESET,
		ValueChange:   -previousValue, // Change to get to zero
		PreviousValue: previousValue,
		NewValue:      newValue,
		Timestamp:     signal.Timestamp,
		Reason:        signal.Reason,
	}

	// Update state
	state.CurrentValue = newValue
	state.History = append(state.History, event)
	state.NextEventId++
	
	// Record transition info
	transitionInfo.EventTriggered = event

	return nil
}

// processUpdateConfigSignal handles updating engine configuration
func processUpdateConfigSignal(state *demov1.DemoEngineState, signal *demov1.UpdateConfigSignal, transitionInfo *demov1.DemoEngineTransitionInfo) error {
	// Validate new configuration
	if signal.Config.DefaultIncrement <= 0 {
		transitionInfo.ValidationPassed = false
		transitionInfo.ValidationMessage = "default increment must be positive"
		return fmt.Errorf("default increment must be positive, got: %d", signal.Config.DefaultIncrement)
	}

	// Check if new max value would invalidate current value
	if signal.Config.MaxValue > 0 && state.CurrentValue > signal.Config.MaxValue {
		transitionInfo.ValidationPassed = false
		transitionInfo.ValidationMessage = fmt.Sprintf("current value %d exceeds new max value %d", state.CurrentValue, signal.Config.MaxValue)
		return fmt.Errorf("current value %d exceeds new max value %d", state.CurrentValue, signal.Config.MaxValue)
	}

	// Check if new negative constraint would invalidate current value
	if !signal.Config.AllowNegative && state.CurrentValue < 0 {
		transitionInfo.ValidationPassed = false
		transitionInfo.ValidationMessage = "current value is negative but new config disallows negative values"
		return fmt.Errorf("current value %d is negative but new config disallows negative values", state.CurrentValue)
	}

	// Create event record (config updates are also tracked as events)
	event := &demov1.CounterEvent{
		EventId:       state.NextEventId,
		Type:          demov1.CounterEventType_COUNTER_EVENT_TYPE_CONFIG_UPDATE,
		ValueChange:   0, // No value change for config updates
		PreviousValue: state.CurrentValue,
		NewValue:      state.CurrentValue,
		Timestamp:     signal.Timestamp,
		Reason:        signal.Reason,
	}

	// Update configuration
	state.Config = signal.Config
	state.History = append(state.History, event)
	state.NextEventId++
	
	// Record transition info
	transitionInfo.EventTriggered = event

	return nil
}

// ApplyBusinessDefaults sets up default state for a new demo engine instance
func ApplyBusinessDefaults(state *demov1.DemoEngineState) *demov1.DemoEngineState {
	if state == nil {
		// Return a new state with defaults
		state = &demov1.DemoEngineState{}
	}

	// Set default configuration if not provided
	if state.Config == nil {
		state.Config = &demov1.DemoConfig{
			MaxValue:         0,  // No limit by default
			DefaultIncrement: 1,  // Default increment of 1
			AllowNegative:    true, // Allow negative values by default
			Description:      "Demo counter engine",
		}
	}

	// Initialize other default values
	if state.NextEventId == 0 {
		state.CurrentValue = 0
		state.NextEventId = 1
		// Don't set LastUpdated here - it will be set when first signal is processed
	}

	// Initialize empty history if nil
	if state.History == nil {
		state.History = make([]*demov1.CounterEvent, 0)
	}

	// Initialize empty metadata if nil
	if state.Metadata == nil {
		state.Metadata = make(map[string]*anypb.Any, 0)
	}

	return state
}

// CompressState implements state compression for continue-as-new operations.
// This method optimizes the demo engine state by:
// - Limiting history to the most recent events
// - Clearing old metadata entries
// - Preserving essential state (current value, config, etc.)
func CompressState(ctx context.Context, currentState *demov1.DemoEngineState) (*demov1.DemoEngineState, error) {
	if currentState == nil {
		return nil, fmt.Errorf("cannot compress nil state")
	}

	// Clone the state to avoid mutating the original
	compressedState := proto.Clone(currentState).(*demov1.DemoEngineState)

	// Configuration for compression thresholds
	const (
		MaxHistoryEvents = 50   // Keep only the last 50 events
		MaxMetadataEntries = 10 // Keep only 10 metadata entries
	)

	// Compress history: keep only the most recent events
	if len(compressedState.History) > MaxHistoryEvents {
		// Keep the most recent events
		recentEvents := compressedState.History[len(compressedState.History)-MaxHistoryEvents:]
		compressedState.History = make([]*demov1.CounterEvent, len(recentEvents))
		copy(compressedState.History, recentEvents)
		
		// Log compression for debugging
		fmt.Printf("Compressed history from %d to %d events\n", 
			len(currentState.History), len(compressedState.History))
	}

	// Compress metadata: keep only recent entries (simple strategy)
	if len(compressedState.Metadata) > MaxMetadataEntries {
		// For demo purposes, just clear old metadata
		// In a real implementation, you might preserve specific keys or use LRU
		compressedState.Metadata = make(map[string]*anypb.Any)
		
		fmt.Printf("Cleared metadata entries due to size (%d entries)\n", 
			len(currentState.Metadata))
	}

	// Ensure essential fields are preserved
	// These should always be maintained across continue-as-new:
	// - CurrentValue (the main counter state)
	// - Config (business rules)
	// - NextEventId (for event ordering)
	// - LastUpdated (for temporal consistency)

	return compressedState, nil
} 