package entityworkflows

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/bo-socayo/amrita-engines/pkg/engines"
	entityv1 "github.com/bo-socayo/amrita-engines/gen/entity/v1"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Helper function for min of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// EntityWorkflowState tracks requests and operations for continue-as-new decisions
type EntityWorkflowState struct {
	requestCount         int
	pendingUpdates       int
	processedRequests    map[string]time.Time // ✅ Track idempotency_key -> processed_time
	requestsBeforeCAN    int                  // Threshold for continue-as-new
	idempotencyCutoff    time.Time            // ✅ Cutoff from previous workflow (inclusive boundary)
	sequenceNumber       int64                // ✅ Monotonic counter preserved across continue-as-new
	entityMetadata       *entityv1.EntityMetadata // ✅ Preserved EntityMetadata across continue-as-new
}

// NewEntityWorkflowState creates initial workflow state
func NewEntityWorkflowState(idempotencyCutoff time.Time) *EntityWorkflowState {
	return &EntityWorkflowState{
		requestCount:         0,
		pendingUpdates:       0,
		processedRequests:    make(map[string]time.Time),
		requestsBeforeCAN:    1000,        // ✅ Reasonable default from docs
		idempotencyCutoff:    idempotencyCutoff, // ✅ Cutoff from previous workflow
		sequenceNumber:       0,           // ✅ Starts at 0 for new workflows
		entityMetadata:       nil,         // ✅ Will be initialized from first RequestContext
	}
}

// NewEntityWorkflowStateForContinuation creates workflow state for continue-as-new with preserved sequence number and metadata
func NewEntityWorkflowStateForContinuation(idempotencyCutoff time.Time, sequenceNumber int64, entityMetadata *entityv1.EntityMetadata) *EntityWorkflowState {
	return &EntityWorkflowState{
		requestCount:         0,           // ✅ Reset counters
		pendingUpdates:       0,           // ✅ Reset counters
		processedRequests:    make(map[string]time.Time), // ✅ Fresh idempotency cache
		requestsBeforeCAN:    1000,        // ✅ Reset threshold
		idempotencyCutoff:    idempotencyCutoff, // ✅ Preserved cutoff
		sequenceNumber:       sequenceNumber,   // ✅ CRITICAL: Preserve monotonic sequence
		entityMetadata:       entityMetadata,   // ✅ CRITICAL: Preserve authorization context
	}
}

// WorkflowIDHandler provides pluggable ID handling for different entity types
type WorkflowIDHandler interface {
	BuildWorkflowID(metadata *entityv1.EntityMetadata) string
	ParseWorkflowID(workflowID string) (entityType, entityID string, err error)
	ExtractContextFromMetadata(metadata *entityv1.EntityMetadata) map[string]string
}

// EntityIDHandler handles standard business entity workflow IDs
type EntityIDHandler struct{}

func (h *EntityIDHandler) BuildWorkflowID(metadata *entityv1.EntityMetadata) string {
	return fmt.Sprintf("%s-%s", metadata.EntityType, metadata.EntityId)
}

func (h *EntityIDHandler) ParseWorkflowID(workflowID string) (entityType, entityID string, err error) {
	parts := strings.SplitN(workflowID, "-", 2)
	if len(parts) < 2 {
		return "", "", fmt.Errorf("invalid entity workflow ID format: %s", workflowID)
	}
	return parts[0], parts[1], nil
}

func (h *EntityIDHandler) ExtractContextFromMetadata(metadata *entityv1.EntityMetadata) map[string]string {
	return map[string]string{
		"entity_type": metadata.EntityType,
		"entity_id":   metadata.EntityId,
		"org_id":      metadata.OrgId,
		"team_id":     metadata.TeamId,
		"user_id":     metadata.UserId,
		"environment": metadata.Environment,
		"tenant":      metadata.Tenant,
	}
}

// newInstance creates a new instance of type T for unmarshaling
func newInstance[T any]() T {
	var zero T
	t := reflect.TypeOf(zero)
	
	// If T is a pointer type, create a new instance of the pointed-to type
	if t != nil && t.Kind() == reflect.Ptr {
		// Create new instance of the element type
		elem := reflect.New(t.Elem())
		instance := elem.Interface().(T)
		
		// If it's a protobuf message, ensure it's properly initialized
		if msg, ok := any(instance).(proto.Message); ok {
			proto.Reset(msg)
		}
		
		return instance
	}
	
	// For non-pointer types, return zero value
	return zero
}

// EntityWorkflowParams contains the parameters for starting an entity workflow
type EntityWorkflowParams[TState proto.Message] struct {
	EntityId      string               `json:"entity_id"`       // Required: Entity identifier  
	InitialState  TState               `json:"initial_state"`   // Optional: Initial state
	WorkflowState *EntityWorkflowState `json:"workflow_state,omitempty"` // Internal: for continue-as-new
}

// EntityQueryResponse contains the full engine state for queries
// Uses JSON string for CurrentState to avoid protobuf oneof field marshaling issues
type EntityQueryResponse[TState proto.Message] struct {
	CurrentStateJSON string                   `json:"current_state_json"`
	Metadata         *entityv1.EntityMetadata `json:"metadata"`
	IsInitialized    bool                     `json:"is_initialized"`
}

// ✅ Use client-provided idempotency key for time-based deduplication with cutoff
func (state *EntityWorkflowState) isRequestProcessed(ctx workflow.Context, idempotencyKey string, requestTime time.Time) bool {
	logger := workflow.GetLogger(ctx)
	
	if idempotencyKey == "" {
		logger.Warn("⚠️ No idempotency key provided - cannot deduplicate")
		return false
	}
	
	// ✅ First check: Is request before/at cutoff from previous workflow? (INCLUSIVE)
	if !requestTime.After(state.idempotencyCutoff) {  // Same as requestTime <= cutoff
		logger.Info("🚫 Request before/at cutoff from previous workflow", 
			"idempotencyKey", idempotencyKey,
			"requestTime", requestTime, 
			"cutoff", state.idempotencyCutoff)
		return true  // Treat as "already processed"
	}
	
	// Second check: Normal deduplication within current workflow
	processedTime, exists := state.processedRequests[idempotencyKey]
	if !exists {
		return false
	}
	
	logger.Info("🔁 Request already processed within current workflow", 
		"idempotencyKey", idempotencyKey, 
		"processedTime", processedTime)
	return true
}

// ✅ Mark request as processed using client idempotency key
func (state *EntityWorkflowState) markRequestProcessed(ctx workflow.Context, idempotencyKey string) {
	logger := workflow.GetLogger(ctx)
	
	if idempotencyKey == "" {
		logger.Warn("⚠️ Cannot mark request as processed - no idempotency key")
		return
	}
	
	state.processedRequests[idempotencyKey] = workflow.Now(ctx)
	logger.Info("✅ Request marked as processed", "idempotencyKey", idempotencyKey)
}

// ✅ Prepare cutoff for next workflow (newest processed request time)
func (state *EntityWorkflowState) getIdempotencyCutoffForContinuation(ctx workflow.Context) time.Time {
	logger := workflow.GetLogger(ctx)
	
	var newestTime time.Time
	for idempotencyKey, processedTime := range state.processedRequests {
		if processedTime.After(newestTime) {
			newestTime = processedTime
			logger.Debug("📅 Found newer cutoff candidate", 
				"idempotencyKey", idempotencyKey, 
				"time", processedTime)
		}
	}
	
	logger.Info("🔄 Prepared idempotency cutoff for continue-as-new", 
		"cutoff", newestTime, 
		"totalProcessed", len(state.processedRequests))
	
	return newestTime
}

// EntityWorkflow is the generic entity workflow that can work with any engine type
// It supports pluggable workflow ID handling for different entity types
func EntityWorkflow[TState, TEvent, TTransitionInfo proto.Message](
	ctx workflow.Context, 
	params EntityWorkflowParams[TState],
	engine engines.Engine[TState, TEvent, TTransitionInfo],
	idHandler WorkflowIDHandler,
) error {
	logger := workflow.GetLogger(ctx)
	
	// Auto-derive entity type from the engine's state protobuf type
	entityType := GetEntityTypeFromProtobuf[TState]()
	
	logger.Info("🚀 EntityWorkflow starting", "entityId", params.EntityId, "entityType", entityType)

	// ✅ Initialize workflow state tracking
	var workflowState *EntityWorkflowState
	if params.WorkflowState != nil {
		workflowState = params.WorkflowState
		logger.Info("📋 Restored workflow state from continue-as-new", "requestCount", workflowState.requestCount)
	} else {
		workflowState = NewEntityWorkflowState(time.Time{}) // No cutoff for new workflows
		logger.Info("🆕 Created new workflow state")
	}
	
	// Initialize EntityMetadata - either from workflowState (continue-as-new) or first RequestContext
	var entityMetadata *entityv1.EntityMetadata
	var metadataInitialized bool
	
	// Check if EntityMetadata was preserved from continue-as-new
	if workflowState.entityMetadata != nil {
		entityMetadata = workflowState.entityMetadata
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
		currentState = newInstance[TState]()
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
				
				entityMetadata = &entityv1.EntityMetadata{
					EntityId:    params.EntityId,
					EntityType:  entityType,
					OrgId:       requestCtx.OrgId,
					TeamId:      requestCtx.TeamId,
					UserId:      requestCtx.UserId,
					Environment: requestCtx.Environment,
					Tenant:      requestCtx.Tenant,
					CreatedAt:   timestamppb.New(workflow.Now(ctx)),
				}
				
				// Store in workflowState for persistence across continue-as-new
				workflowState.entityMetadata = entityMetadata
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
				
				if !authorizeRequest(requestCtx, entityMetadata) {
					logger.Error("❌ Authorization failed - RequestContext doesn't match EntityMetadata",
						"requestOrgId", requestCtx.OrgId,
						"entityOrgId", entityMetadata.OrgId,
						"requestTenant", requestCtx.Tenant,
						"entityTenant", entityMetadata.Tenant)
					return zero, fmt.Errorf("authorization failed: tenant/org mismatch")
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
				if workflowState.isRequestProcessed(ctx, requestCtx.IdempotencyKey, requestCtx.RequestTime.AsTime()) {
					logger.Info("🔁 Request already processed (idempotent)", "idempotencyKey", requestCtx.IdempotencyKey)
					return currentState, nil
				}
			}

			// ✅ Track request and pending operations
			workflowState.requestCount++
			workflowState.pendingUpdates++
			defer func() {
				workflowState.pendingUpdates--
			}()

			logger.Info("🔢 Processing request", "requestCount", workflowState.requestCount, "idempotencyKey", requestCtx.GetIdempotencyKey())
			
			// ✅ Increment internal sequence number for this event (preserved across continue-as-new)
			workflowState.sequenceNumber++
			
			logger.Info("📦 Creating typed event envelope", "sequenceNumber", workflowState.sequenceNumber)
			// Create typed event envelope (passing nil for metadata for now)
			envelope, err := engines.NewTypedEventEnvelope(
				workflowState.sequenceNumber,
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
				workflowState.markRequestProcessed(ctx, requestCtx.IdempotencyKey)
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
				if workflowState.requestCount >= workflowState.requestsBeforeCAN {
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
		if workflowState.shouldContinueAsNew(ctx) {
			logger.Info("🔄 Continue-as-new triggered", 
				"requestCount", workflowState.requestCount,
				"pendingUpdates", workflowState.pendingUpdates,
				"temporalSuggested", workflow.GetInfo(ctx).GetContinueAsNewSuggested())
			break
		}

		// Sleep indefinitely until woken by signals/updates
		logger.Info("🌙 EntityWorkflow await mode", 
			"requestCount", workflowState.requestCount,
			"pendingUpdates", workflowState.pendingUpdates)
		workflow.Await(ctx, func() bool { 
			return workflowState.shouldContinueAsNew(ctx)
		})
	}

	// ✅ Wait for all pending updates to complete before continuing
	logger.Info("⏳ Waiting for pending updates to complete", "pendingUpdates", workflowState.pendingUpdates)
	err = workflow.Await(ctx, func() bool {
		return workflowState.pendingUpdates == 0
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
	cutoffForNext := workflowState.getIdempotencyCutoffForContinuation(ctx)
	newWorkflowState := NewEntityWorkflowStateForContinuation(cutoffForNext, workflowState.sequenceNumber, workflowState.entityMetadata)
	
	// ✅ Continue as new with compressed state and preserved workflow state
	newParams := EntityWorkflowParams[TState]{
		EntityId:      params.EntityId,    // Entity ID persists
		InitialState:  compressedState,    // ✅ Use compressed state from engine
		WorkflowState: newWorkflowState,   // ✅ Fresh workflow state with preserved sequence + cutoff
	}

	logger.Info("🔄 Continuing as new", 
		"requestCount", workflowState.requestCount,
		"sequenceNumber", workflowState.sequenceNumber,
		"idempotencyCutoff", cutoffForNext)

	return workflow.NewContinueAsNewError(ctx, EntityWorkflow[TState, TEvent, TTransitionInfo], newParams, engine, idHandler)
}

// ✅ SIMPLIFIED: Remove complex deterministic ID generation
func (state *EntityWorkflowState) shouldContinueAsNew(ctx workflow.Context) bool {
	// Use Temporal's built-in suggestion (recommended)
	if workflow.GetInfo(ctx).GetContinueAsNewSuggested() {
		return true
	}
	
	// Fallback to request count threshold
	return state.requestCount >= state.requestsBeforeCAN
}

// GetEntityTypeFromProtobuf derives the entity type from a protobuf message
func GetEntityTypeFromProtobuf[T proto.Message]() string {
	instance := newInstance[T]()
	descriptor := instance.ProtoReflect().Descriptor()
	return string(descriptor.FullName())
}

// ✅ SIMPLIFIED: Standard entity workflow ID (no request hashing)
func BuildEntityWorkflowIDFromProtobuf[T proto.Message](entityID string) string {
	entityType := GetEntityTypeFromProtobuf[T]()
	return fmt.Sprintf("%s-%s", entityType, entityID)
}

// ✅ REMOVED: All the complex deterministic ID generation helpers
// - BuildDeterministicWorkflowID() 
// - BuildEntityWorkflowIDFromParts()
// - Complex parsing logic

// authorizeRequest validates that the RequestContext matches the established EntityMetadata
func authorizeRequest(requestCtx *entityv1.RequestContext, entityMetadata *entityv1.EntityMetadata) bool {
	if requestCtx == nil || entityMetadata == nil {
		return false
	}
	
	// Check all authorization fields must match
	return requestCtx.OrgId == entityMetadata.OrgId &&
		   requestCtx.TeamId == entityMetadata.TeamId &&
		   requestCtx.UserId == entityMetadata.UserId &&
		   requestCtx.Tenant == entityMetadata.Tenant &&
		   requestCtx.Environment == entityMetadata.Environment
}

// Helper function to parse entity workflow ID
func ParseEntityWorkflowID(workflowID string) (entityType, entityID string, err error) {
	// This is a simple implementation - you might want more sophisticated parsing
	// for complex entity types with dots/hyphens
	parts := strings.SplitN(workflowID, "-", 2)
	if len(parts) < 2 {
		return "", "", fmt.Errorf("invalid entity workflow ID format: %s", workflowID)
	}
	return parts[0], parts[1], nil
}

// EntityWorkflowStarter helps start typed entity workflows
type EntityWorkflowStarter[TState, TEvent, TTransitionInfo proto.Message] struct {
	Engine    engines.Engine[TState, TEvent, TTransitionInfo]
	IDHandler WorkflowIDHandler
}

// NewEntityWorkflowStarter creates a new starter for the given engine type
func NewEntityWorkflowStarter[TState, TEvent, TTransitionInfo proto.Message](
	engine engines.Engine[TState, TEvent, TTransitionInfo],
	idHandler WorkflowIDHandler,
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
	return EntityWorkflow[TState, TEvent, TTransitionInfo](ctx, params, s.Engine, s.IDHandler)
} 