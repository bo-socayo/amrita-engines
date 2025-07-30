package handlers

import (
	"context"
	"fmt"

	"github.com/bo-socayo/amrita-engines/pkg/engines"
	"github.com/bo-socayo/amrita-engines/pkg/entityworkflows/auth"
	"github.com/bo-socayo/amrita-engines/pkg/entityworkflows/state"
	entityv1 "github.com/bo-socayo/amrita-engines/gen/entity/v1"
	"go.temporal.io/sdk/workflow"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// EntityUpdateHandler implements the UpdateHandler interface
type EntityUpdateHandler[TState, TEvent, TTransitionInfo proto.Message] struct {
	engine          engines.Engine[TState, TEvent, TTransitionInfo]
	authorizer      auth.Authorizer
	metadataManager auth.MetadataManager
	workflowState   *state.WorkflowState
	entityType      string
	entityID        string
}

// NewEntityUpdateHandler creates a new update handler
func NewEntityUpdateHandler[TState, TEvent, TTransitionInfo proto.Message](
	engine engines.Engine[TState, TEvent, TTransitionInfo],
	authorizer auth.Authorizer,
	metadataManager auth.MetadataManager,
	workflowState *state.WorkflowState,
	entityType string,
	entityID string,
) *EntityUpdateHandler[TState, TEvent, TTransitionInfo] {
	return &EntityUpdateHandler[TState, TEvent, TTransitionInfo]{
		engine:          engine,
		authorizer:      authorizer,
		metadataManager: metadataManager,
		workflowState:   workflowState,
		entityType:      entityType,
		entityID:        entityID,
	}
}

// HandleProcessEvent handles the processEvent update
func (uh *EntityUpdateHandler[TState, TEvent, TTransitionInfo]) HandleProcessEvent(
	ctx workflow.Context,
	requestCtx *entityv1.RequestContext,
	event TEvent,
	initialized bool,
	currentState TState,
	entityMetadata *entityv1.EntityMetadata,
	metadataInitialized *bool,
) (TState, *entityv1.EntityMetadata, error) {
	logger := workflow.GetLogger(ctx)
	logger.Info("📨 ProcessEvent handler called")

	var zero TState
	if !initialized {
		logger.Error("❌ ProcessEvent called but entity not initialized")
		return zero, entityMetadata, fmt.Errorf("entity not initialized")
	}

	// ✅ Initialize or validate EntityMetadata from RequestContext
	if !*metadataInitialized {
		// First request - initialize EntityMetadata from RequestContext
		if requestCtx == nil {
			logger.Error("❌ First request must include RequestContext for authorization")
			return zero, entityMetadata, fmt.Errorf("first request must include RequestContext")
		}

		// Use metadata manager to initialize EntityMetadata
		var err error
		entityMetadata, err = uh.metadataManager.InitializeFromRequest(
			uh.entityID,
			uh.entityType,
			requestCtx,
			workflow.Now(ctx),
		)
		if err != nil {
			logger.Error("❌ Failed to initialize EntityMetadata", "error", err)
			return zero, entityMetadata, fmt.Errorf("failed to initialize entity metadata: %w", err)
		}

		// Store in workflowState for persistence across continue-as-new
		uh.workflowState.SetEntityMetadata(entityMetadata)
		*metadataInitialized = true

		logger.Info("✅ EntityMetadata initialized from first RequestContext",
			"orgId", entityMetadata.OrgId,
			"teamId", entityMetadata.TeamId,
			"userId", entityMetadata.UserId,
			"tenant", entityMetadata.Tenant)
	} else {
		// Subsequent requests - validate authorization
		if requestCtx == nil {
			logger.Error("❌ RequestContext required for authorization")
			return zero, entityMetadata, fmt.Errorf("RequestContext required")
		}

		// Use authorizer to validate request
		err := uh.authorizer.AuthorizeRequest(requestCtx, entityMetadata)
		if err != nil {
			logger.Error("❌ Authorization failed - RequestContext doesn't match EntityMetadata",
				"requestOrgId", requestCtx.OrgId,
				"entityOrgId", entityMetadata.OrgId,
				"requestTenant", requestCtx.Tenant,
				"entityTenant", entityMetadata.Tenant,
				"error", err)
			return zero, entityMetadata, fmt.Errorf("authorization failed: %w", err)
		}
		logger.Debug("✅ Authorization validated")
	}

	// ✅ Check idempotency using RequestContext
	if requestCtx != nil && requestCtx.IdempotencyKey != "" {
		if uh.workflowState.IsRequestProcessed(ctx, requestCtx.IdempotencyKey, requestCtx.RequestTime.AsTime()) {
			logger.Info("🔁 Request already processed (idempotent)", "idempotencyKey", requestCtx.IdempotencyKey)
			return currentState, entityMetadata, nil
		}
	}

	// ✅ Track request and pending operations
	uh.workflowState.IncrementRequestCount()
	uh.workflowState.IncrementPendingUpdates()
	defer func() {
		uh.workflowState.DecrementPendingUpdates()
	}()

	logger.Info("🔢 Processing request", "requestCount", uh.workflowState.GetRequestCount(), "idempotencyKey", requestCtx.GetIdempotencyKey())

	// ✅ Increment internal sequence number for this event (preserved across continue-as-new)
	sequenceNumber := uh.workflowState.IncrementSequenceNumber()

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
		return zero, entityMetadata, fmt.Errorf("failed to create event envelope: %w", err)
	}

	logger.Info("⚙️  Processing event through engine")
	// Process event through engine - IDIOMATIC GO: returns (state, transitionInfo, error)
	engineCtx := context.Background()
	newState, transitionInfo, err := uh.engine.ProcessEvent(engineCtx, envelope)
	if err != nil {
		logger.Error("❌ Engine ProcessEvent failed", "error", err)
		return zero, entityMetadata, fmt.Errorf("failed to process event: %w", err)
	}

	logger.Info("📊 Processing completed successfully")

	// ✅ Mark request as processed for idempotency (if we have request context)
	if requestCtx != nil && requestCtx.IdempotencyKey != "" {
		uh.workflowState.MarkRequestProcessed(ctx, requestCtx.IdempotencyKey)
	}

	// Log transition info for debugging/monitoring
	logger.Info("✅ Event processed successfully",
		"transition", transitionInfo,
		"newState", newState,
		"idempotencyKey", requestCtx.GetIdempotencyKey())

	// Return the new state directly
	return newState, entityMetadata, nil
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