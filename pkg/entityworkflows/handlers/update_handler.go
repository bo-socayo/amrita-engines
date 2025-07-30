package handlers

import (
	"context"
	"fmt"

	"github.com/bo-socayo/amrita-engines/pkg/engines"
	"github.com/bo-socayo/amrita-engines/pkg/entityworkflows/state"
	entityv1 "github.com/bo-socayo/amrita-engines/gen/entity/v1"
	"go.temporal.io/sdk/workflow"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// EntityUpdateHandler handles core event processing through the engine
type EntityUpdateHandler[TState, TEvent, TTransitionInfo proto.Message] struct {
	engine     engines.Engine[TState, TEvent, TTransitionInfo]
	entityType string
}

// NewEntityUpdateHandler creates a new update handler
func NewEntityUpdateHandler[TState, TEvent, TTransitionInfo proto.Message](
	engine engines.Engine[TState, TEvent, TTransitionInfo],
	entityType string,
) *EntityUpdateHandler[TState, TEvent, TTransitionInfo] {
	return &EntityUpdateHandler[TState, TEvent, TTransitionInfo]{
		engine:     engine,
		entityType: entityType,
	}
}

// ProcessEvent handles the core event processing logic (engine interaction)
func (uh *EntityUpdateHandler[TState, TEvent, TTransitionInfo]) ProcessEvent(
	ctx workflow.Context,
	event TEvent,
	sequenceNumber int64,
) (TState, interface{}, error) {
	logger := workflow.GetLogger(ctx)
	var zero TState

	logger.Info("📦 Creating typed event envelope", "sequenceNumber", sequenceNumber)
	// Create typed event envelope (passing nil for metadata for now)
	envelope, err := engines.NewTypedEventEnvelope(
		sequenceNumber,
		fmt.Sprintf("%s.event", uh.entityType),
		timestamppb.New(workflow.Now(ctx)),
		event,
		nil, // TODO: Convert map[string]string metadata to protobuf
	)
	if err != nil {
		logger.Error("❌ Failed to create event envelope", "error", err)
		return zero, nil, fmt.Errorf("failed to create event envelope: %w", err)
	}

	logger.Info("⚙️  Processing event through engine")
	// Process event through engine - IDIOMATIC GO: returns (state, transitionInfo, error)
	engineCtx := context.Background()
	newState, transitionInfo, err := uh.engine.ProcessEvent(engineCtx, envelope)
	if err != nil {
		logger.Error("❌ Engine ProcessEvent failed", "error", err)
		return zero, nil, fmt.Errorf("failed to process event: %w", err)
	}

	logger.Info("📊 Processing completed successfully")
	logger.Info("✅ Event processed successfully",
		"transition", transitionInfo,
		"newState", newState)

	return newState, transitionInfo, nil
}

// UpdateValidatorImpl implements the UpdateValidator interface
type UpdateValidatorImpl struct {
	workflowState *state.WorkflowState
}

// NewUpdateValidator creates a new update validator
func NewUpdateValidator(workflowState *state.WorkflowState) *UpdateValidatorImpl {
	return &UpdateValidatorImpl{
		workflowState: workflowState,
	}
}

// ValidateUpdate validates that updates are allowed (not during continue-as-new)
func (uv *UpdateValidatorImpl) ValidateUpdate(ctx workflow.Context, requestCtx *entityv1.RequestContext) error {
	if uv.workflowState.GetRequestCount() >= uv.workflowState.GetRequestsBeforeCAN() {
		return fmt.Errorf("backoff for continue-as-new")
	}
	return nil
} 