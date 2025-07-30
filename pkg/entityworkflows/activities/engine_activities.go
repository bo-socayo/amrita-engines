package activities

import (
	"context"
	"fmt"

	"github.com/bo-socayo/amrita-engines/pkg/engines"
	"github.com/bo-socayo/amrita-engines/pkg/entityworkflows/utils"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// ProcessEventActivityInput contains all data needed for engine processing
type ProcessEventActivityInput[TEvent proto.Message] struct {
	EntityType     string                               `json:"entity_type"`
	SequenceNumber int64                                `json:"sequence_number"`
	EventEnvelope  *engines.TypedEventEnvelope[TEvent]  `json:"event_envelope"`
	CurrentState   []byte                               `json:"current_state"` // Serialized state
	CreatedAt      *timestamppb.Timestamp               `json:"created_at"`
}

// ProcessEventActivityResult contains the results of engine processing
type ProcessEventActivityResult struct {
	NewState       []byte `json:"new_state"`        // Serialized state
	TransitionInfo []byte `json:"transition_info"`  // Serialized transition info
	SequenceNumber int64  `json:"sequence_number"`
	Success        bool   `json:"success"`
	ErrorMessage   string `json:"error_message,omitempty"`
}

// ProcessEventActivity wraps engine.ProcessEvent for deterministic execution
// This is a generic activity that works with any engine type
func ProcessEventActivity[TState, TEvent, TTransitionInfo proto.Message](
	ctx context.Context, 
	input ProcessEventActivityInput[TEvent],
) (*ProcessEventActivityResult, error) {
	// Get engine instance from registry
	engine, err := GetEngineForType[TState, TEvent, TTransitionInfo](input.EntityType)
	if err != nil {
		return &ProcessEventActivityResult{
			Success:        false,
			ErrorMessage:   fmt.Sprintf("failed to get engine for type %s: %v", input.EntityType, err),
			SequenceNumber: input.SequenceNumber,
		}, nil
	}

	// Deserialize current state
	var currentState TState
	if len(input.CurrentState) > 0 {
		currentState = utils.NewInstance[TState]()
		if err := proto.Unmarshal(input.CurrentState, currentState); err != nil {
			return &ProcessEventActivityResult{
				Success:        false,
				ErrorMessage:   fmt.Sprintf("failed to deserialize current state: %v", err),
				SequenceNumber: input.SequenceNumber,
			}, nil
		}
	} else {
		currentState = utils.NewInstance[TState]()
	}

	// Initialize engine with current state if not already initialized
	if !engine.IsInitialized() {
		finalState, err := engine.SetInitialState(ctx, currentState, input.CreatedAt)
		if err != nil {
			return &ProcessEventActivityResult{
				Success:        false,
				ErrorMessage:   fmt.Sprintf("engine initialization failed: %v", err),
				SequenceNumber: input.SequenceNumber,
			}, nil
		}
		currentState = finalState
	}

	// Process event through engine
	newState, transitionInfo, err := engine.ProcessEvent(ctx, input.EventEnvelope)
	if err != nil {
		return &ProcessEventActivityResult{
			Success:        false,
			ErrorMessage:   fmt.Sprintf("engine processing failed: %v", err),
			SequenceNumber: input.SequenceNumber,
		}, nil
	}

	// Serialize results
	newStateBytes, err := proto.Marshal(newState)
	if err != nil {
		return &ProcessEventActivityResult{
			Success:        false,
			ErrorMessage:   fmt.Sprintf("failed to serialize new state: %v", err),
			SequenceNumber: input.SequenceNumber,
		}, nil
	}

	transitionInfoBytes, err := proto.Marshal(transitionInfo)
	if err != nil {
		return &ProcessEventActivityResult{
			Success:        false,
			ErrorMessage:   fmt.Sprintf("failed to serialize transition info: %v", err),
			SequenceNumber: input.SequenceNumber,
		}, nil
	}

	return &ProcessEventActivityResult{
		NewState:       newStateBytes,
		TransitionInfo: transitionInfoBytes,
		SequenceNumber: input.SequenceNumber,
		Success:        true,
	}, nil
} 