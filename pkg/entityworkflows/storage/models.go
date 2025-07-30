package storage

import (
	"encoding/json"
	"errors"
	"time"
)

// Validation errors
var (
	ErrMissingEntityID          = errors.New("entity_id is required")
	ErrMissingEntityType        = errors.New("entity_type is required")
	ErrInvalidSequenceNumber    = errors.New("sequence_number must be non-negative")
	ErrMissingProcessedAt       = errors.New("processed_at is required")
	ErrMissingCurrentState      = errors.New("current_state is required")
	ErrMissingEventEnvelope     = errors.New("event_envelope is required")
)

// EntityWorkflowRecord represents a complete snapshot of entity workflow state
type EntityWorkflowRecord struct {
	// Primary identifiers
	EntityID       string `json:"entity_id" db:"entity_id"`
	EntityType     string `json:"entity_type" db:"entity_type"`
	SequenceNumber int64  `json:"sequence_number" db:"sequence_number"`
	
	// Timestamps
	ProcessedAt time.Time `json:"processed_at" db:"processed_at"`
	EventTime   time.Time `json:"event_time" db:"event_time"`
	
	// Entity metadata
	OrgID  string `json:"org_id" db:"org_id"`
	TeamID string `json:"team_id" db:"team_id"`
	UserID string `json:"user_id" db:"user_id"`
	Tenant string `json:"tenant" db:"tenant"`
	
	// State data
	CurrentState      []byte `json:"current_state" db:"current_state"`           // Serialized protobuf
	PreviousState     []byte `json:"previous_state" db:"previous_state"`         // For audit trails
	StateTypeName     string `json:"state_type_name" db:"state_type_name"`
	
	// Request/Response data
	EventEnvelope     []byte `json:"event_envelope" db:"event_envelope"`         // Serialized request
	TransitionInfo    []byte `json:"transition_info" db:"transition_info"`       // Serialized response
	EventTypeName     string `json:"event_type_name" db:"event_type_name"`
	
	// Idempotency
	IdempotencyKey string `json:"idempotency_key" db:"idempotency_key"`
	
	// Workflow tracking
	WorkflowID string `json:"workflow_id" db:"workflow_id"`
	RunID      string `json:"run_id" db:"run_id"`
	
	// Business logic versioning
	BusinessLogicVersion string `json:"business_logic_version" db:"business_logic_version"`
	
	// Indexing and search
	Tags map[string]string `json:"tags" db:"tags"` // JSON column for custom indexing
}

// TagsAsJSON serializes the tags map to JSON for database storage
func (r *EntityWorkflowRecord) TagsAsJSON() ([]byte, error) {
	if r.Tags == nil {
		return []byte("{}"), nil
	}
	return json.Marshal(r.Tags)
}

// SetTagsFromJSON deserializes JSON tags into the Tags map
func (r *EntityWorkflowRecord) SetTagsFromJSON(data []byte) error {
	if len(data) == 0 {
		r.Tags = make(map[string]string)
		return nil
	}
	return json.Unmarshal(data, &r.Tags)
}

// SetTag adds or updates a tag
func (r *EntityWorkflowRecord) SetTag(key, value string) {
	if r.Tags == nil {
		r.Tags = make(map[string]string)
	}
	r.Tags[key] = value
}

// GetTag retrieves a tag value
func (r *EntityWorkflowRecord) GetTag(key string) (string, bool) {
	if r.Tags == nil {
		return "", false
	}
	value, exists := r.Tags[key]
	return value, exists
}

// Validate checks if the record has all required fields
func (r *EntityWorkflowRecord) Validate() error {
	if r.EntityID == "" {
		return ErrMissingEntityID
	}
	if r.EntityType == "" {
		return ErrMissingEntityType
	}
	if r.SequenceNumber < 0 {
		return ErrInvalidSequenceNumber
	}
	if r.ProcessedAt.IsZero() {
		return ErrMissingProcessedAt
	}
	if len(r.CurrentState) == 0 {
		return ErrMissingCurrentState
	}
	if len(r.EventEnvelope) == 0 {
		return ErrMissingEventEnvelope
	}
	return nil
} 