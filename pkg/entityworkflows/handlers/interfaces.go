package handlers

import (
	entityv1 "github.com/bo-socayo/amrita-engines/gen/entity/v1"
	"go.temporal.io/sdk/workflow"
	"google.golang.org/protobuf/proto"
)

// QueryHandler defines the interface for handling workflow queries
type QueryHandler[TState proto.Message] interface {
	HandleGetEntityState(initialized bool, currentState TState, entityMetadata *entityv1.EntityMetadata) (*EntityQueryResponse[TState], error)
}

// UpdateHandler defines the interface for handling workflow updates
type UpdateHandler[TState, TEvent proto.Message] interface {
	HandleProcessEvent(
		ctx workflow.Context,
		requestCtx *entityv1.RequestContext,
		event TEvent,
		initialized bool,
		currentState TState,
	) (TState, error)
}

// UpdateValidator defines the interface for validating updates
type UpdateValidator interface {
	ValidateUpdate(ctx workflow.Context, requestCtx *entityv1.RequestContext) error
}

// EntityQueryResponse contains the full engine state for queries
// Uses JSON string for CurrentState to avoid protobuf oneof field marshaling issues
type EntityQueryResponse[TState proto.Message] struct {
	CurrentStateJSON string                   `json:"current_state_json"`
	Metadata         *entityv1.EntityMetadata `json:"metadata"`
	IsInitialized    bool                     `json:"is_initialized"`
}

// WorkflowLogger abstracts workflow logging for testing
type WorkflowLogger interface {
	Info(msg string, keyvals ...interface{})
	Error(msg string, keyvals ...interface{})
	Debug(msg string, keyvals ...interface{})
	Warn(msg string, keyvals ...interface{})
}

// WorkflowContext abstracts workflow context for testing
type WorkflowContext interface {
	GetLogger() WorkflowLogger
	Now() interface{} // Returns time.Time in real implementation
} 