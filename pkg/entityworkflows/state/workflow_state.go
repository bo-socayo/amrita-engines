package state

import (
	"time"
	entityv1 "github.com/bo-socayo/amrita-engines/gen/entity/v1"
	"go.temporal.io/sdk/workflow"
)

// WorkflowState tracks requests and operations for continue-as-new decisions
// This is the implementation of the StateManager interface
type WorkflowState struct {
	requestCount         int
	pendingUpdates       int
	processedRequests    map[string]time.Time // Track idempotency_key -> processed_time
	requestsBeforeCAN    int                  // Threshold for continue-as-new
	idempotencyCutoff    time.Time            // Cutoff from previous workflow (inclusive boundary)
	sequenceNumber       int64                // Monotonic counter preserved across continue-as-new
	entityMetadata       *entityv1.EntityMetadata // Preserved EntityMetadata across continue-as-new
}

// NewWorkflowState creates initial workflow state
func NewWorkflowState(idempotencyCutoff time.Time) *WorkflowState {
	return &WorkflowState{
		requestCount:         0,
		pendingUpdates:       0,
		processedRequests:    make(map[string]time.Time),
		requestsBeforeCAN:    1000,        // Reasonable default from docs
		idempotencyCutoff:    idempotencyCutoff, // Cutoff from previous workflow
		sequenceNumber:       0,           // Starts at 0 for new workflows
		entityMetadata:       nil,         // Will be initialized from first RequestContext
	}
}

// NewWorkflowStateForContinuation creates workflow state for continue-as-new with preserved sequence number and metadata
func NewWorkflowStateForContinuation(idempotencyCutoff time.Time, sequenceNumber int64, entityMetadata *entityv1.EntityMetadata) *WorkflowState {
	return &WorkflowState{
		requestCount:         0,           // Reset counters
		pendingUpdates:       0,           // Reset counters
		processedRequests:    make(map[string]time.Time), // Fresh idempotency cache
		requestsBeforeCAN:    1000,        // Reset threshold
		idempotencyCutoff:    idempotencyCutoff, // Preserved cutoff
		sequenceNumber:       sequenceNumber,   // CRITICAL: Preserve monotonic sequence
		entityMetadata:       entityMetadata,   // CRITICAL: Preserve authorization context
	}
}

// ShouldContinueAsNew determines if workflow should continue as new
func (s *WorkflowState) ShouldContinueAsNew(ctx WorkflowContextInterface) bool {
	// Use Temporal's built-in suggestion (recommended)
	if ctx.GetContinueAsNewSuggested() {
		return true
	}
	
	// Fallback to request count threshold
	return s.requestCount >= s.requestsBeforeCAN
}

// Adapter method for Temporal workflow.Context
func (s *WorkflowState) ShouldContinueAsNewWithWorkflowContext(ctx workflow.Context) bool {
	// Use Temporal's built-in suggestion (recommended)
	if workflow.GetInfo(ctx).GetContinueAsNewSuggested() {
		return true
	}
	
	// Fallback to request count threshold
	return s.requestCount >= s.requestsBeforeCAN
}

// IncrementRequestCount increments the request counter
func (s *WorkflowState) IncrementRequestCount() {
	s.requestCount++
}

// IncrementSequenceNumber increments and returns the sequence number
func (s *WorkflowState) IncrementSequenceNumber() int64 {
	s.sequenceNumber++
	return s.sequenceNumber
}

// GetSequenceNumber returns the current sequence number
func (s *WorkflowState) GetSequenceNumber() int64 {
	return s.sequenceNumber
}

// GetRequestCount returns the current request count
func (s *WorkflowState) GetRequestCount() int {
	return s.requestCount
}

// GetPendingUpdates returns the current pending updates count
func (s *WorkflowState) GetPendingUpdates() int {
	return s.pendingUpdates
}

// IncrementPendingUpdates increments the pending updates counter
func (s *WorkflowState) IncrementPendingUpdates() {
	s.pendingUpdates++
}

// DecrementPendingUpdates decrements the pending updates counter
func (s *WorkflowState) DecrementPendingUpdates() {
	s.pendingUpdates--
}

// SetEntityMetadata sets the entity metadata
func (s *WorkflowState) SetEntityMetadata(metadata *entityv1.EntityMetadata) {
	s.entityMetadata = metadata
}

// GetEntityMetadata returns the entity metadata
func (s *WorkflowState) GetEntityMetadata() *entityv1.EntityMetadata {
	return s.entityMetadata
}

// GetProcessedRequests returns the processed requests map (for idempotency checks)
func (s *WorkflowState) GetProcessedRequests() map[string]time.Time {
	return s.processedRequests
}

// GetIdempotencyCutoff returns the idempotency cutoff time
func (s *WorkflowState) GetIdempotencyCutoff() time.Time {
	return s.idempotencyCutoff
}

// GetRequestsBeforeCAN returns the threshold for continue-as-new
func (s *WorkflowState) GetRequestsBeforeCAN() int {
	return s.requestsBeforeCAN
}

// IsRequestProcessed checks if a request has been processed using workflow.Context
func (s *WorkflowState) IsRequestProcessed(ctx workflow.Context, idempotencyKey string, requestTime time.Time) bool {
	logger := workflow.GetLogger(ctx)
	
	if idempotencyKey == "" {
		logger.Warn("⚠️ No idempotency key provided - cannot deduplicate")
		return false
	}
	
	// First check: Is request before/at cutoff from previous workflow? (INCLUSIVE)
	if !requestTime.After(s.idempotencyCutoff) {  // Same as requestTime <= cutoff
		logger.Info("🚫 Request before/at cutoff from previous workflow", 
			"idempotencyKey", idempotencyKey,
			"requestTime", requestTime, 
			"cutoff", s.idempotencyCutoff)
		return true  // Treat as "already processed"
	}
	
	// Second check: Normal deduplication within current workflow
	processedTime, exists := s.processedRequests[idempotencyKey]
	if !exists {
		return false
	}
	
	logger.Info("🔁 Request already processed within current workflow", 
		"idempotencyKey", idempotencyKey, 
		"processedTime", processedTime)
	return true
}

// MarkRequestProcessed marks a request as processed using workflow.Context
func (s *WorkflowState) MarkRequestProcessed(ctx workflow.Context, idempotencyKey string) {
	logger := workflow.GetLogger(ctx)
	
	if idempotencyKey == "" {
		logger.Warn("⚠️ Cannot mark request as processed - no idempotency key")
		return
	}
	
	s.processedRequests[idempotencyKey] = workflow.Now(ctx)
	logger.Info("✅ Request marked as processed", "idempotencyKey", idempotencyKey)
}

// GetIdempotencyCutoffForContinuation prepares cutoff for next workflow using workflow.Context
func (s *WorkflowState) GetIdempotencyCutoffForContinuation(ctx workflow.Context) time.Time {
	logger := workflow.GetLogger(ctx)
	
	var newestTime time.Time
	for idempotencyKey, processedTime := range s.processedRequests {
		if processedTime.After(newestTime) {
			newestTime = processedTime
			logger.Debug("📅 Found newer cutoff candidate", 
				"idempotencyKey", idempotencyKey, 
				"time", processedTime)
		}
	}
	
	logger.Info("🔄 Prepared idempotency cutoff for continue-as-new", 
		"cutoff", newestTime, 
		"totalProcessed", len(s.processedRequests))
	
	return newestTime
} 