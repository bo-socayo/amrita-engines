package handlers

import (
	"fmt"
	"reflect"

	entityv1 "github.com/bo-socayo/amrita-engines/gen/entity/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// EntityQueryHandler implements the QueryHandler interface
type EntityQueryHandler[TState proto.Message] struct{}

// NewEntityQueryHandler creates a new query handler
func NewEntityQueryHandler[TState proto.Message]() *EntityQueryHandler[TState] {
	return &EntityQueryHandler[TState]{}
}

// HandleGetEntityState handles the getEntityState query
func (qh *EntityQueryHandler[TState]) HandleGetEntityState(
	initialized bool,
	currentState TState,
	entityMetadata *entityv1.EntityMetadata,
	entityID string,
) (*EntityQueryResponse[TState], error) {
	if !initialized {
		// Return zero state for uninitialized workflows
		var zeroState TState
		stateJSON, err := protojson.Marshal(zeroState)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal zero state to JSON: %w", err)
		}

		return &EntityQueryResponse[TState]{
			CurrentStateJSON: string(stateJSON),
			Metadata:         entityMetadata, // May be nil if no requests received yet
			IsInitialized:    false,
		}, nil
	}

	// Validate current state for initialized workflows
	currentStateValue := reflect.ValueOf(currentState)
	if currentStateValue.IsNil() {
		return nil, fmt.Errorf("current state is nil")
	}

	// Marshal current state to JSON
	stateJSON, err := protojson.Marshal(currentState)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal current state to JSON: %w", err)
	}

	return &EntityQueryResponse[TState]{
		CurrentStateJSON: string(stateJSON),
		Metadata:         entityMetadata,
		IsInitialized:    true,
	}, nil
}

// GetPreviewString returns a preview of the JSON for logging
func GetPreviewString(jsonData []byte, maxLength int) string {
	if len(jsonData) <= maxLength {
		return string(jsonData)
	}
	return string(jsonData[:maxLength])
}

// min returns the minimum of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
} 