package entityworkflows

import (
	"context"
	"fmt"
	"reflect"

	"github.com/bo-socayo/amrita-engines/pkg/engines"
	"github.com/bo-socayo/amrita-engines/pkg/entityworkflows/auth"
	"github.com/bo-socayo/amrita-engines/pkg/entityworkflows/handlers"
	"github.com/bo-socayo/amrita-engines/pkg/entityworkflows/ids"
	"github.com/bo-socayo/amrita-engines/pkg/entityworkflows/state"
	"github.com/bo-socayo/amrita-engines/pkg/entityworkflows/utils"
	entityv1 "github.com/bo-socayo/amrita-engines/gen/entity/v1"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Type aliases for backward compatibility - now imported from other packages
type WorkflowIDHandler = ids.WorkflowIDHandler
type EntityIDHandler = ids.EntityIDHandler
type EntityWorkflowParams[TState proto.Message] = utils.EntityWorkflowParams[TState]
type EntityQueryResponse[TState proto.Message] = handlers.EntityQueryResponse[TState]

// Function aliases for backward compatibility
var NewEntityIDHandler = ids.NewEntityIDHandler
// Note: GetEntityTypeFromProtobuf is a generic function and must be called directly from ids package

// Idempotency methods are now in the state package

// EntityWorkflow is the generic entity workflow that can work with any engine type
// It supports pluggable workflow ID handling for different entity types
func EntityWorkflow[TState, TEvent, TTransitionInfo proto.Message](
	ctx workflow.Context, 
	params utils.EntityWorkflowParams[TState],
	engine engines.Engine[TState, TEvent, TTransitionInfo],
	idHandler ids.WorkflowIDHandler,
) error {
	logger := workflow.GetLogger(ctx)
	
	// Auto-derive entity type from the engine's state protobuf type
	entityType := ids.GetEntityTypeFromProtobuf[TState]()
	
	logger.Info("🚀 EntityWorkflow starting", "entityId", params.EntityId, "entityType", entityType)

	// ✅ Initialize workflow state tracking
	var workflowState *state.WorkflowState
	if params.WorkflowState != nil {
		workflowState = params.WorkflowState
		logger.Info("📋 Restored workflow state from continue-as-new", "requestCount", workflowState.GetRequestCount())
	} else {
		workflowState = state.NewWorkflowState(workflow.Now(ctx)) // Fresh workflow cutoff at start time
		logger.Info("🆕 Created new workflow state", "cutoff", workflow.Now(ctx))
	}
	
	// ✅ Initialize authorization and metadata managers
	authorizer := auth.NewRequestAuthorizer()
	metadataManager := auth.NewEntityMetadataManager()
	
	// Initialize EntityMetadata - either from workflowState (continue-as-new) or first RequestContext
	var entityMetadata *entityv1.EntityMetadata
	var metadataInitialized bool
	
	// Check if EntityMetadata was preserved from continue-as-new
	if workflowState.GetEntityMetadata() != nil {
		entityMetadata = workflowState.GetEntityMetadata()
		metadataInitialized = true
		logger.Info("✅ EntityMetadata restored from continue-as-new", 
			"orgId", entityMetadata.OrgId, 
			"teamId", entityMetadata.TeamId,
			"userId", entityMetadata.UserId,
			"tenant", entityMetadata.Tenant)
	}
	
	// === TEMPORAL WORKFLOW STATE MANAGEMENT ===
	// State that persists across replays (workflow variables)
	var currentState TState
	var compressedState TState
	var initialized bool

	// Initialize state from parameters (this persists across replays)
	if !reflect.ValueOf(params.InitialState).IsNil() {
		currentState = params.InitialState
		logger.Info("📋 Using provided initial state")
	} else {
		currentState = utils.NewInstance[TState]()
		logger.Info("🆕 Creating new instance for initial state")
	}

	// === ENGINE INITIALIZATION (happens on EVERY execution/replay) ===
	// IDIOMATIC GO: Always initialize the engine with current workflow state
	// Temporal ensures this is deterministic across replays
	logger.Info("🔧 Starting engine initialization")
	engineCtx := context.Background()
	
	// Use creation time from entityMetadata if available, otherwise current workflow time
	var createdAt *timestamppb.Timestamp
	if entityMetadata != nil && entityMetadata.CreatedAt != nil {
		createdAt = entityMetadata.CreatedAt
	} else {
		createdAt = timestamppb.New(workflow.Now(ctx))
	}
	
	finalState, err := engine.SetInitialState(engineCtx, currentState, createdAt)
	if err != nil {
		logger.Error("❌ Engine initialization failed", "error", err)
		return fmt.Errorf("failed to initialize engine: %w", err)
	}
	logger.Info("✅ Engine initialization completed")
	
	// Update current state with the initialized state
	currentState = finalState
	initialized = true
	logger.Info("🎯 Workflow state updated and marked as initialized")

	// ✅ Workflow mutex for state protection
	stateMutex := workflow.NewMutex(ctx)

	// Register query handler to return full engine state
	workflow.SetQueryHandler(ctx, "getEntityState", func() (*EntityQueryResponse[TState], error) {
		logger.Info("🔍 Query handler called", "initialized", initialized, "currentStateNil", reflect.ValueOf(currentState).IsNil())
		
		if !initialized {
			logger.Info("❌ Workflow not initialized, returning zero state")
			var zeroState TState
			// Serialize zero state to JSON using protojson
			stateJSON, err := protojson.Marshal(zeroState)
			if err != nil {
				logger.Error("❌ Failed to marshal zero state to JSON", "error", err)
				return nil, fmt.Errorf("failed to marshal zero state to JSON: %w", err)
			}
			logger.Info("✅ Zero state marshaled", "jsonLength", len(stateJSON))
			return &EntityQueryResponse[TState]{
				CurrentStateJSON: string(stateJSON),
				Metadata:         entityMetadata, // May be nil if no requests received yet
				IsInitialized:    false,
			}, nil
		}

		// Serialize current state to JSON using protojson to handle oneof fields properly
		currentStateValue := reflect.ValueOf(currentState)
		logger.Info("🔍 Attempting to marshal current state to JSON", 
			"stateType", reflect.TypeOf(currentState).String(),
			"stateIsNil", currentStateValue.IsNil(),
			"stateValue", fmt.Sprintf("%+v", currentState))
		
		if currentStateValue.IsNil() {
			logger.Error("❌ Current state is nil even though initialized=true")
			return nil, fmt.Errorf("current state is nil")
		}
		
		stateJSON, err := protojson.Marshal(currentState)
		if err != nil {
			logger.Error("❌ Failed to marshal state to JSON", "error", err)
			return nil, fmt.Errorf("failed to marshal current state to JSON: %w", err)
		}
		
					logger.Info("✅ Successfully marshaled state to JSON", 
				"jsonLength", len(stateJSON), 
				"isEmpty", len(stateJSON) == 0,
				"preview", string(stateJSON)[:min(len(stateJSON), 200)])

			response := &EntityQueryResponse[TState]{
				CurrentStateJSON: string(stateJSON),
				Metadata:         entityMetadata,
				IsInitialized:    true,
			}
			
			var entityId string
			if entityMetadata != nil {
				entityId = entityMetadata.EntityId
			} else {
				entityId = params.EntityId
			}
			
			logger.Info("🎯 Returning query response", 
				"responseJSONLength", len(response.CurrentStateJSON),
				"metadataEntityId", entityId)
		
		return response, nil
	})

	// ✅ Register update handler with proper authorization and RequestContext
	err = workflow.SetUpdateHandlerWithOptions(ctx, "processEvent", 
		func(ctx workflow.Context, requestCtx *entityv1.RequestContext, event TEvent) (TState, error) {
			logger := workflow.GetLogger(ctx)
			logger.Info("📨 ProcessEvent handler called")
			
			var zero TState
			if !initialized {
				logger.Error("❌ ProcessEvent called but entity not initialized")
				return zero, fmt.Errorf("entity not initialized")
			}

			// ✅ Initialize or validate EntityMetadata from RequestContext
			if !metadataInitialized {
				// First request - initialize EntityMetadata from RequestContext
				if requestCtx == nil {
					logger.Error("❌ First request must include RequestContext for authorization")
					return zero, fmt.Errorf("first request must include RequestContext")
				}
				
				// Use metadata manager to initialize EntityMetadata
				var err error
				entityMetadata, err = metadataManager.InitializeFromRequest(
					params.EntityId,
					entityType,
					requestCtx,
					workflow.Now(ctx),
				)
				if err != nil {
					logger.Error("❌ Failed to initialize EntityMetadata", "error", err)
					return zero, fmt.Errorf("failed to initialize entity metadata: %w", err)
				}
				
				// Store in workflowState for persistence across continue-as-new
				workflowState.SetEntityMetadata(entityMetadata)
				metadataInitialized = true
				
				logger.Info("✅ EntityMetadata initialized from first RequestContext", 
					"orgId", entityMetadata.OrgId, 
					"teamId", entityMetadata.TeamId,
					"userId", entityMetadata.UserId,
					"tenant", entityMetadata.Tenant)
			} else {
				// Subsequent requests - validate authorization
				if requestCtx == nil {
					logger.Error("❌ RequestContext required for authorization")
					return zero, fmt.Errorf("RequestContext required")
				}
				
				// Use authorizer to validate request
				err := authorizer.AuthorizeRequest(requestCtx, entityMetadata)
				if err != nil {
					logger.Error("❌ Authorization failed - RequestContext doesn't match EntityMetadata",
						"requestOrgId", requestCtx.OrgId,
						"entityOrgId", entityMetadata.OrgId,
						"requestTenant", requestCtx.Tenant,
						"entityTenant", entityMetadata.Tenant,
						"error", err)
					return zero, fmt.Errorf("authorization failed: %w", err)
				}
				logger.Debug("✅ Authorization validated")
			}

			// ✅ Acquire mutex for concurrent protection
			err := stateMutex.Lock(ctx)
			if err != nil {
				return zero, err
			}
			defer stateMutex.Unlock()

			// ✅ Check idempotency using RequestContext
			if requestCtx != nil && requestCtx.IdempotencyKey != "" {
				if workflowState.IsRequestProcessed(ctx, requestCtx.IdempotencyKey, requestCtx.RequestTime.AsTime()) {
					logger.Info("🔁 Request already processed (idempotent)", "idempotencyKey", requestCtx.IdempotencyKey)
					return currentState, nil
				}
			}

			// ✅ Track request and pending operations
			workflowState.IncrementRequestCount()
			workflowState.IncrementPendingUpdates()
			defer func() {
				workflowState.DecrementPendingUpdates()
			}()

			logger.Info("🔢 Processing request", "requestCount", workflowState.GetRequestCount(), "idempotencyKey", requestCtx.GetIdempotencyKey())
			
			// ✅ Increment internal sequence number for this event (preserved across continue-as-new)
			sequenceNumber := workflowState.IncrementSequenceNumber()
			
			logger.Info("📦 Creating typed event envelope", "sequenceNumber", sequenceNumber)
			// Create typed event envelope (passing nil for metadata for now)
			envelope, err := engines.NewTypedEventEnvelope(
				sequenceNumber,
				fmt.Sprintf("%s.event", entityType),
				timestamppb.New(workflow.Now(ctx)),
				event,
				nil, // TODO: Convert map[string]string metadata to protobuf
			)
			if err != nil {
				logger.Error("❌ Failed to create event envelope", "error", err)
				return zero, fmt.Errorf("failed to create event envelope: %w", err)
			}

			logger.Info("⚙️  Processing event through engine")
			// Process event through engine - IDIOMATIC GO: returns (state, transitionInfo, error)
			engineCtx := context.Background()
			newState, transitionInfo, err := engine.ProcessEvent(engineCtx, envelope)
			if err != nil {
				logger.Error("❌ Engine ProcessEvent failed", "error", err)
				return zero, fmt.Errorf("failed to process event: %w", err)
			}

			logger.Info("📊 Processing completed successfully")
			// Update current state - no unmarshaling needed, we get the typed state directly
			currentState = newState
			
			// ✅ Mark request as processed for idempotency (if we have request context)
			if requestCtx != nil && requestCtx.IdempotencyKey != "" {
				workflowState.MarkRequestProcessed(ctx, requestCtx.IdempotencyKey)
			}
			
			// Log transition info for debugging/monitoring
			logger.Info("✅ Event processed successfully", 
				"transition", transitionInfo,
				"newState", currentState,
				"idempotencyKey", requestCtx.GetIdempotencyKey())
			
			// Return the new state directly
			return currentState, nil
		},
		workflow.UpdateHandlerOptions{
			// ✅ Validator prevents updates during continue-as-new
			Validator: func(ctx workflow.Context, requestCtx *entityv1.RequestContext, event TEvent) error {
				if workflowState.GetRequestCount() >= workflowState.GetRequestsBeforeCAN() {
					return temporal.NewApplicationError("Backoff for continue-as-new", "ErrBackoff")
				}
				return nil
			},
		})
	
	if err != nil {
		logger.Error("❌ Failed to register processEvent update handler", "error", err)
		return fmt.Errorf("failed to register update handler: %w", err)
	}
	logger.Info("✅ processEvent update handler registered successfully")

	// ✅ Removed batch processing for simplicity

	// ✅ Main workflow loop with proper continue-as-new logic
	for {
		// ✅ Check if we should continue as new
		if workflowState.ShouldContinueAsNewWithWorkflowContext(ctx) {
			logger.Info("🔄 Continue-as-new triggered", 
				"requestCount", workflowState.GetRequestCount(),
				"pendingUpdates", workflowState.GetPendingUpdates(),
				"temporalSuggested", workflow.GetInfo(ctx).GetContinueAsNewSuggested())
			break
		}

		// Sleep indefinitely until woken by signals/updates
		logger.Info("🌙 EntityWorkflow await mode", 
			"requestCount", workflowState.GetRequestCount(),
			"pendingUpdates", workflowState.GetPendingUpdates())
		workflow.Await(ctx, func() bool { 
			return workflowState.ShouldContinueAsNewWithWorkflowContext(ctx)
		})
	}

	// ✅ Wait for all pending updates to complete before continuing
	logger.Info("⏳ Waiting for pending updates to complete", "pendingUpdates", workflowState.GetPendingUpdates())
	err = workflow.Await(ctx, func() bool {
		return workflowState.GetPendingUpdates() == 0
	})
	if err != nil {
		logger.Error("❌ Failed to wait for pending updates", "error", err)
		return err
	}

	// ✅ Give engine a chance to compress/optimize state before continue-as-new
	logger.Info("🗜️  Calling engine CompressState hook before continue-as-new")
	engineCtx = context.Background()
	compressedState, err = engine.CompressState(engineCtx, currentState)
	if err != nil {
		logger.Error("❌ Engine CompressState failed", "error", err)
		return fmt.Errorf("failed to compress state for continue-as-new: %w", err)
	}
	
	// ✅ Prepare cutoff for next workflow and create new state with preserved sequence number and metadata
	cutoffForNext := workflowState.GetIdempotencyCutoffForContinuation(ctx)
	newWorkflowState := state.NewWorkflowStateForContinuation(cutoffForNext, workflowState.GetSequenceNumber(), workflowState.GetEntityMetadata())
	
	// ✅ Continue as new with compressed state and preserved workflow state
	newParams := EntityWorkflowParams[TState]{
		EntityId:      params.EntityId,    // Entity ID persists
		InitialState:  compressedState,    // ✅ Use compressed state from engine
		WorkflowState: newWorkflowState,   // ✅ Fresh workflow state with preserved sequence + cutoff
	}

	logger.Info("🔄 Continuing as new", 
		"requestCount", workflowState.GetRequestCount(),
		"sequenceNumber", workflowState.GetSequenceNumber(),
		"idempotencyCutoff", cutoffForNext)

	return workflow.NewContinueAsNewError(ctx, EntityWorkflow[TState, TEvent, TTransitionInfo], newParams, engine, idHandler)
}

// Continue-as-new logic is now in the state package

// Legacy functions moved to other packages - kept for backward compatibility
// Note: BuildEntityWorkflowIDFromProtobuf is a generic function and must be called directly from ids package
var ParseEntityWorkflowID = ids.ParseEntityWorkflowID
// Note: NewEntityWorkflowStarter is a generic function and must be called directly from utils package

// EntityWorkflowStarter type alias for backward compatibility
type EntityWorkflowStarter[TState, TEvent, TTransitionInfo proto.Message] = utils.EntityWorkflowStarter[TState, TEvent, TTransitionInfo] 