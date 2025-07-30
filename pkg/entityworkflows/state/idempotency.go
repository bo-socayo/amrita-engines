package state

import (
	"time"
	"go.temporal.io/sdk/workflow"
)

// IdempotencyChecker handles request deduplication
type IdempotencyChecker struct {
	processedRequests map[string]time.Time
	cutoff           time.Time
}

// NewIdempotencyChecker creates a new idempotency checker
func NewIdempotencyChecker(cutoff time.Time) *IdempotencyChecker {
	return &IdempotencyChecker{
		processedRequests: make(map[string]time.Time),
		cutoff:           cutoff,
	}
}

// IsRequestProcessed checks if a request has been processed using the testable interface
func (ic *IdempotencyChecker) IsRequestProcessed(ctx WorkflowContextInterface, key string, requestTime time.Time) bool {
	if key == "" {
		// Cannot deduplicate without key
		return false
	}
	
	// First check: Is request before/at cutoff from previous workflow? (INCLUSIVE)
	if !requestTime.After(ic.cutoff) {  // Same as requestTime <= cutoff
		return true  // Treat as "already processed"
	}
	
	// Second check: Normal deduplication within current workflow
	_, exists := ic.processedRequests[key]
	return exists
}

// IsRequestProcessedWithWorkflowContext checks if a request has been processed using workflow.Context
func (ic *IdempotencyChecker) IsRequestProcessedWithWorkflowContext(ctx workflow.Context, key string, requestTime time.Time) bool {
	logger := workflow.GetLogger(ctx)
	
	if key == "" {
		logger.Warn("⚠️ No idempotency key provided - cannot deduplicate")
		return false
	}
	
	// First check: Is request before/at cutoff from previous workflow? (INCLUSIVE)
	if !requestTime.After(ic.cutoff) {  // Same as requestTime <= cutoff
		logger.Info("🚫 Request before/at cutoff from previous workflow", 
			"idempotencyKey", key,
			"requestTime", requestTime, 
			"cutoff", ic.cutoff)
		return true  // Treat as "already processed"
	}
	
	// Second check: Normal deduplication within current workflow
	processedTime, exists := ic.processedRequests[key]
	if !exists {
		return false
	}
	
	logger.Info("🔁 Request already processed within current workflow", 
		"idempotencyKey", key, 
		"processedTime", processedTime)
	return true
}

// MarkRequestProcessed marks a request as processed using the testable interface
func (ic *IdempotencyChecker) MarkRequestProcessed(ctx WorkflowContextInterface, key string) {
	if key == "" {
		// Cannot mark request without key
		return
	}
	
	ic.processedRequests[key] = ctx.Now()
}

// MarkRequestProcessedWithWorkflowContext marks a request as processed using workflow.Context
func (ic *IdempotencyChecker) MarkRequestProcessedWithWorkflowContext(ctx workflow.Context, key string) {
	logger := workflow.GetLogger(ctx)
	
	if key == "" {
		logger.Warn("⚠️ Cannot mark request as processed - no idempotency key")
		return
	}
	
	ic.processedRequests[key] = workflow.Now(ctx)
	logger.Info("✅ Request marked as processed", "idempotencyKey", key)
}

// GetCutoffForContinuation prepares cutoff for next workflow (newest processed request time)
func (ic *IdempotencyChecker) GetCutoffForContinuation(ctx WorkflowContextInterface) time.Time {
	var newestTime time.Time
	for _, processedTime := range ic.processedRequests {
		if processedTime.After(newestTime) {
			newestTime = processedTime
		}
	}
	
	return newestTime
}

// GetCutoffForContinuationWithWorkflowContext prepares cutoff for next workflow using workflow.Context
func (ic *IdempotencyChecker) GetCutoffForContinuationWithWorkflowContext(ctx workflow.Context) time.Time {
	logger := workflow.GetLogger(ctx)
	
	var newestTime time.Time
	for idempotencyKey, processedTime := range ic.processedRequests {
		if processedTime.After(newestTime) {
			newestTime = processedTime
			logger.Debug("📅 Found newer cutoff candidate", 
				"idempotencyKey", idempotencyKey, 
				"time", processedTime)
		}
	}
	
	logger.Info("🔄 Prepared idempotency cutoff for continue-as-new", 
		"cutoff", newestTime, 
		"totalProcessed", len(ic.processedRequests))
	
	return newestTime
}

// GetProcessedRequests returns a copy of the processed requests map
func (ic *IdempotencyChecker) GetProcessedRequests() map[string]time.Time {
	result := make(map[string]time.Time)
	for k, v := range ic.processedRequests {
		result[k] = v
	}
	return result
}

// GetCutoff returns the cutoff time
func (ic *IdempotencyChecker) GetCutoff() time.Time {
	return ic.cutoff
} 