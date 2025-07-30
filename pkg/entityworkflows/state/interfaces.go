package state

import (
	"time"
	entityv1 "github.com/bo-socayo/amrita-engines/gen/entity/v1"
)

// WorkflowContextInterface abstracts the workflow context for testing
type WorkflowContextInterface interface {
	GetContinueAsNewSuggested() bool
	Now() time.Time
}

// StateManager defines the interface for workflow state management
type StateManager interface {
	ShouldContinueAsNew(ctx WorkflowContextInterface) bool
	IncrementRequestCount()
	IncrementSequenceNumber() int64
	GetSequenceNumber() int64
	GetRequestCount() int
	GetPendingUpdates() int
	IncrementPendingUpdates()
	DecrementPendingUpdates()
	SetEntityMetadata(metadata *entityv1.EntityMetadata)
	GetEntityMetadata() *entityv1.EntityMetadata
}

// IdempotencyService defines the interface for request deduplication
type IdempotencyService interface {
	IsRequestProcessed(ctx WorkflowContextInterface, key string, requestTime time.Time) bool
	MarkRequestProcessed(ctx WorkflowContextInterface, key string)
	GetCutoffForContinuation(ctx WorkflowContextInterface) time.Time
}

// ContinuationService defines the interface for continue-as-new logic
type ContinuationService interface {
	ShouldContinue(ctx WorkflowContextInterface, requestCount int) bool
	PrepareForContinuation(
		state StateManager,
		idempotency IdempotencyService,
	) (time.Time, int64, *entityv1.EntityMetadata)
} 