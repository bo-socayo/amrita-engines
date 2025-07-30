package utils

import (
	"github.com/bo-socayo/amrita-engines/pkg/engines"
	"github.com/bo-socayo/amrita-engines/pkg/entityworkflows/ids"
	"github.com/bo-socayo/amrita-engines/pkg/entityworkflows/state"
	"go.temporal.io/sdk/workflow"
	"google.golang.org/protobuf/proto"
)

// EntityWorkflowParams contains the parameters for starting an entity workflow
type EntityWorkflowParams[TState proto.Message] struct {
	EntityId      string               `json:"entity_id"`       // Required: Entity identifier  
	InitialState  TState               `json:"initial_state"`   // Optional: Initial state
	WorkflowState *state.WorkflowState `json:"workflow_state,omitempty"` // Internal: for continue-as-new
}

// EntityWorkflowStarter helps start typed entity workflows
type EntityWorkflowStarter[TState, TEvent, TTransitionInfo proto.Message] struct {
	Engine    engines.Engine[TState, TEvent, TTransitionInfo]
	IDHandler ids.WorkflowIDHandler
}

// NewEntityWorkflowStarter creates a new starter for the given engine type
func NewEntityWorkflowStarter[TState, TEvent, TTransitionInfo proto.Message](
	engine engines.Engine[TState, TEvent, TTransitionInfo],
	idHandler ids.WorkflowIDHandler,
) *EntityWorkflowStarter[TState, TEvent, TTransitionInfo] {
	return &EntityWorkflowStarter[TState, TEvent, TTransitionInfo]{
		Engine:    engine,
		IDHandler: idHandler,
	}
}

// StartWorkflow starts an entity workflow with the given parameters
func (s *EntityWorkflowStarter[TState, TEvent, TTransitionInfo]) StartWorkflow(
	ctx workflow.Context,
	params EntityWorkflowParams[TState],
) error {
	// Note: This would need to import the main EntityWorkflow function
	// For now, this is just the structure
	return nil
}

// Min returns the minimum of two integers
func Min(a, b int) int {
	if a < b {
		return a
	}
	return b
} 