package activities

import (
	"context"
	"fmt"
	"time"

	"github.com/bo-socayo/amrita-engines/pkg/engines"
	"github.com/bo-socayo/amrita-engines/pkg/entityworkflows/storage"
	"github.com/bo-socayo/amrita-engines/pkg/entityworkflows/utils"
	"google.golang.org/protobuf/proto"
)

// StoreEntityWorkflowRecordInput contains all data needed for storage
type StoreEntityWorkflowRecordInput[TEvent proto.Message] struct {
	// Entity identification
	EntityID       string `json:"entity_id"`
	EntityType     string `json:"entity_type"`
	SequenceNumber int64  `json:"sequence_number"`
	
	// Timestamps
	EventTime time.Time `json:"event_time"`
	
	// Entity metadata
	OrgID  string `json:"org_id"`
	TeamID string `json:"team_id"`
	UserID string `json:"user_id"`
	Tenant string `json:"tenant"`
	
	// State data
	CurrentState      []byte `json:"current_state"`        // Serialized protobuf
	PreviousState     []byte `json:"previous_state"`       // For audit trails
	StateTypeName     string `json:"state_type_name"`
	
	// Request/Response data
	EventEnvelope     *engines.TypedEventEnvelope[TEvent] `json:"event_envelope"`
	TransitionInfo    []byte                              `json:"transition_info"`
	EventTypeName     string                              `json:"event_type_name"`
	
	// Idempotency
	IdempotencyKey string `json:"idempotency_key"`
	
	// Workflow tracking
	WorkflowID string `json:"workflow_id"`
	RunID      string `json:"run_id"`
	
	// Business logic versioning
	BusinessLogicVersion string `json:"business_logic_version"`
}

// StoreEntityWorkflowRecordResult contains the results of storage operation
type StoreEntityWorkflowRecordResult struct {
	Success      bool   `json:"success"`
	ErrorMessage string `json:"error_message,omitempty"`
}

// StoreEntityWorkflowRecordActivity stores entity workflow record to configured storage adapter
func StoreEntityWorkflowRecordActivity[TEvent proto.Message](
	ctx context.Context,
	input StoreEntityWorkflowRecordInput[TEvent],
) (*StoreEntityWorkflowRecordResult, error) {
	// Get storage adapter for entity type
	storageAdapter := storage.GetStorageAdapter(input.EntityType)
	if storageAdapter == nil {
		// No storage configured for this entity type - this is not an error
		return &StoreEntityWorkflowRecordResult{
			Success: true, // Success because no storage was expected
		}, nil
	}

	// Serialize event envelope
	eventEnvelopeBytes, err := proto.Marshal(input.EventEnvelope)
	if err != nil {
		return &StoreEntityWorkflowRecordResult{
			Success:      false,
			ErrorMessage: fmt.Sprintf("failed to serialize event envelope: %v", err),
		}, nil
	}

	// Create storage record
	record := &storage.EntityWorkflowRecord{
		EntityID:             input.EntityID,
		EntityType:           input.EntityType,
		SequenceNumber:       input.SequenceNumber,
		ProcessedAt:          time.Now(),
		EventTime:            input.EventTime,
		OrgID:                input.OrgID,
		TeamID:               input.TeamID,
		UserID:               input.UserID,
		Tenant:               input.Tenant,
		CurrentState:         input.CurrentState,
		PreviousState:        input.PreviousState,
		StateTypeName:        input.StateTypeName,
		EventEnvelope:        eventEnvelopeBytes,
		TransitionInfo:       input.TransitionInfo,
		EventTypeName:        input.EventTypeName,
		IdempotencyKey:       input.IdempotencyKey,
		WorkflowID:           input.WorkflowID,
		RunID:                input.RunID,
		BusinessLogicVersion: input.BusinessLogicVersion,
	}

	// Store the record
	if err := storageAdapter.StoreEntityWorkflowRecord(ctx, record); err != nil {
		return &StoreEntityWorkflowRecordResult{
			Success:      false,
			ErrorMessage: fmt.Sprintf("failed to store record: %v", err),
		}, nil
	}

	return &StoreEntityWorkflowRecordResult{
		Success: true,
	}, nil
}

// getStateTypeName extracts the protobuf type name for the state
func getStateTypeName[TState proto.Message]() string {
	return string(utils.NewInstance[TState]().ProtoReflect().Descriptor().Name())
}

// getEventTypeName extracts the protobuf type name for the event
func getEventTypeName[TEvent proto.Message]() string {
	return string(utils.NewInstance[TEvent]().ProtoReflect().Descriptor().Name())
} 