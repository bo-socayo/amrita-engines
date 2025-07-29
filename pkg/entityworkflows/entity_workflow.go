package entityworkflows

import (
	"context"
	"fmt"
	"reflect"
	"strings"

	"github.com/bo-socayo/amrita-engines/pkg/engines"
	entityv1 "github.com/bo-socayo/amrita-engines/gen/entity/v1"
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
		"signal_type": metadata.SignalType,
		"entity_id":   metadata.EntityId,
		"org_id":      metadata.OrgId,
		"team_id":     metadata.TeamId,
		"user_id":     metadata.UserId,
		"environment": metadata.Environment,
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
	InitialState   TState                   `json:"initial_state"`
	Metadata       *entityv1.EntityMetadata `json:"metadata"`
}

// EntityQueryResponse contains the full engine state for queries
// Uses JSON string for CurrentState to avoid protobuf oneof field marshaling issues
type EntityQueryResponse[TState proto.Message] struct {
	CurrentStateJSON string                   `json:"current_state_json"`
	Metadata         *entityv1.EntityMetadata `json:"metadata"`
	IsInitialized    bool                     `json:"is_initialized"`
}

// EntityWorkflow is the generic entity workflow that can work with any engine type
// It supports pluggable workflow ID handling for different entity types
func EntityWorkflow[TState, TEvent, TTransitionInfo proto.Message](
	ctx workflow.Context, 
	params EntityWorkflowParams[TState],
	engine engines.Engine[TState, TEvent, TTransitionInfo],
	idHandler WorkflowIDHandler,
) error {
	// Auto-derive entity type from the engine's state protobuf type
	params.Metadata.EntityType = GetEntityTypeFromProtobuf[TState]()
	
	// Automatically set search attributes based on metadata for queryability
	// Note: Using only registered Text search attributes (max 3 in dev/SQLite)
	// searchAttributes := map[string]interface{}{
	// 	"TenantId":   params.Metadata.Tenant,
	// 	"OrgId":      params.Metadata.OrgId,
	// 	"EntityType": params.Metadata.EntityType,
	// }
	
	// Note: UserId, TeamId, Environment not included due to local Temporal dev limits
	// In production with PostgreSQL/MySQL, you can register more search attributes
	
	logger := workflow.GetLogger(ctx)
	logger.Info("🚀 EntityWorkflow starting", "entityId", params.Metadata.EntityId, "entityType", params.Metadata.EntityType)

	// Update search attributes for this workflow
	// err := workflow.UpsertSearchAttributes(ctx, searchAttributes)
	// if err != nil {
	// 	return fmt.Errorf("failed to set search attributes: %w", err)
	// }
	// logger.Info("✅ Search attributes updated")
	
	// === TEMPORAL WORKFLOW STATE MANAGEMENT ===
	// State that persists across replays (workflow variables)
	var currentState TState
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
	finalState, err := engine.SetInitialState(engineCtx, currentState, params.Metadata.CreatedAt)
	if err != nil {
		logger.Error("❌ Engine initialization failed", "error", err)
		return fmt.Errorf("failed to initialize engine: %w", err)
	}
	logger.Info("✅ Engine initialization completed")
	
	// Update current state with the initialized state
	currentState = finalState
	initialized = true
	logger.Info("🎯 Workflow state updated and marked as initialized")

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
				Metadata:         params.Metadata,
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
			Metadata:         params.Metadata,
			IsInitialized:    true,
		}
		
		logger.Info("🎯 Returning query response", 
			"responseJSONLength", len(response.CurrentStateJSON),
			"metadataEntityId", response.Metadata.EntityId)
		
		return response, nil
	})

	// Register update handler for processing events - returns new state directly (idiomatic Go)
	workflow.SetUpdateHandler(ctx, "processEvent", func(ctx workflow.Context, event TEvent, metadata map[string]string) (TState, error) {
		logger := workflow.GetLogger(ctx)
		logger.Info("📨 ProcessEvent handler called")
		
		var zero TState
		if !initialized {
			logger.Error("❌ ProcessEvent called but entity not initialized")
			return zero, fmt.Errorf("entity not initialized")
		}

		logger.Info("🔢 Incrementing sequence number", "current", params.Metadata.SequenceNumber)
		// Increment sequence number for this event
		params.Metadata.SequenceNumber++
		
		logger.Info("📦 Creating typed event envelope")
		// Create typed event envelope (passing nil for metadata for now)
		envelope, err := engines.NewTypedEventEnvelope(
			params.Metadata.SequenceNumber,
			fmt.Sprintf("%s.event", params.Metadata.EntityType),
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

		logger.Info("📅 Updating metadata timestamps")
		// Update metadata timestamp
		params.Metadata.UpdatedAt = timestamppb.New(workflow.Now(ctx))
		params.Metadata.Timestamp = timestamppb.New(workflow.Now(ctx))

		// Update current state - no unmarshaling needed, we get the typed state directly
		currentState = newState
		
		// Log transition info for debugging/monitoring
		logger.Info("✅ Event processed successfully", 
			"transition", transitionInfo,
			"newState", currentState)
		
		// Return the new state directly
		return currentState, nil
	})

	// Register update handler for batch processing - returns new state directly (idiomatic Go)
	workflow.SetUpdateHandler(ctx, "processEvents", func(ctx workflow.Context, events []TEvent, metadata []map[string]string) (TState, error) {
		var zero TState
		if !initialized {
			return zero, fmt.Errorf("entity not initialized")
		}

		// Create typed event envelopes
		envelopes := make([]*engines.TypedEventEnvelope[TEvent], len(events))
		for i, event := range events {
			// Increment sequence number for each event
			params.Metadata.SequenceNumber++

			envelope, err := engines.NewTypedEventEnvelope(
				params.Metadata.SequenceNumber,
				fmt.Sprintf("%s.event", params.Metadata.EntityType),
				timestamppb.New(workflow.Now(ctx)),
				event,
				nil, // TODO: Convert map[string]string metadata to protobuf
			)
			if err != nil {
				return zero, fmt.Errorf("failed to create event envelope %d: %w", i, err)
			}
			envelopes[i] = envelope
		}

		// Process events through engine - IDIOMATIC GO: returns (state, transitionInfos, error)
		engineCtx := context.Background()
		newState, transitionInfos, err := engine.ProcessEvents(engineCtx, envelopes)
		if err != nil {
			return zero, fmt.Errorf("failed to process events: %w", err)
		}

		// Update metadata timestamp
		params.Metadata.UpdatedAt = timestamppb.New(workflow.Now(ctx))
		params.Metadata.Timestamp = timestamppb.New(workflow.Now(ctx))

		// Update current state - no unmarshaling needed, we get the typed state directly
		currentState = newState
		
		// Log transition info for debugging/monitoring
		workflow.GetLogger(ctx).Info("Batch events processed", 
			"eventCount", len(events),
			"transitionCount", len(transitionInfos),
			"newState", currentState)

		// Return the new state directly
		return currentState, nil
	})

	// Main workflow loop
	for {
		// Continue-as-new if Temporal workflow history gets too long
		// Note: We use a simple heuristic since workflow.GetInfo(ctx) doesn't have HistoryLength
		// In production, you'd want to use Temporal's built-in continue-as-new suggestions
		historyEventCount := workflow.GetInfo(ctx).Attempt // Simple proxy for history growth
		if historyEventCount > 100 { // Lower threshold for demo purposes
			newParams := EntityWorkflowParams[TState]{
				InitialState: currentState,
				Metadata:     params.Metadata, // Sequence number persists across continue-as-new
			}

			return workflow.NewContinueAsNewError(ctx, EntityWorkflow[TState, TEvent, TTransitionInfo], newParams, engine, idHandler)
		}

		// Sleep indefinitely until woken by signals/updates
		logger.Info("🌙 EntityWorkflow initialization complete - entering await mode")
		workflow.Await(ctx, func() bool { return false })
	}
}

// GetEntityTypeFromProtobuf derives the entity type from a protobuf message
func GetEntityTypeFromProtobuf[T proto.Message]() string {
	instance := newInstance[T]()
	descriptor := instance.ProtoReflect().Descriptor()
	return string(descriptor.FullName())
}

// BuildEntityWorkflowIDFromProtobuf builds workflow ID using protobuf type derivation
func BuildEntityWorkflowIDFromProtobuf[T proto.Message](entityID string) string {
	entityType := GetEntityTypeFromProtobuf[T]()
	return fmt.Sprintf("%s-%s", entityType, entityID)
}

// Helper function to build entity workflow ID from metadata (deprecated - use BuildEntityWorkflowIDFromProtobuf)
func BuildEntityWorkflowID(metadata *entityv1.EntityMetadata) string {
	return fmt.Sprintf("%s-%s", metadata.EntityType, metadata.EntityId)
}

// Legacy helper function to build entity workflow ID from separate parameters
func BuildEntityWorkflowIDFromParts(entityType, entityID string) string {
	return fmt.Sprintf("%s-%s", entityType, entityID)
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