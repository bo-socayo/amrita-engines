package state

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	entityv1 "github.com/bo-socayo/amrita-engines/gen/entity/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// MockWorkflowContext implements WorkflowContextInterface for testing
type MockWorkflowContext struct {
	mock.Mock
	currentTime            time.Time
	continueAsNewSuggested bool
}

func NewMockWorkflowContext() *MockWorkflowContext {
	return &MockWorkflowContext{
		currentTime: time.Now(),
	}
}

func (m *MockWorkflowContext) Now() time.Time {
	return m.currentTime
}

func (m *MockWorkflowContext) SetTime(t time.Time) {
	m.currentTime = t
}

func (m *MockWorkflowContext) GetContinueAsNewSuggested() bool {
	return m.continueAsNewSuggested
}

func (m *MockWorkflowContext) SetContinueAsNewSuggested(suggested bool) {
	m.continueAsNewSuggested = suggested
}

// Test WorkflowState

func TestNewWorkflowState(t *testing.T) {
	cutoff := time.Now()
	state := NewWorkflowState(cutoff)

	assert.NotNil(t, state)
	assert.Equal(t, 0, state.GetRequestCount())
	assert.Equal(t, 0, state.GetPendingUpdates())
	assert.Equal(t, int64(0), state.GetSequenceNumber())
	assert.Equal(t, cutoff, state.GetIdempotencyCutoff())
	assert.Equal(t, 1000, state.GetRequestsBeforeCAN())
	assert.Nil(t, state.GetEntityMetadata())
	assert.NotNil(t, state.GetProcessedRequests())
	assert.Len(t, state.GetProcessedRequests(), 0)
}

func TestNewWorkflowStateForContinuation(t *testing.T) {
	cutoff := time.Now()
	sequenceNumber := int64(42)
	metadata := &entityv1.EntityMetadata{
		EntityId: "test-entity",
		EntityType: "test.Entity",
	}

	state := NewWorkflowStateForContinuation(cutoff, sequenceNumber, metadata)

	assert.NotNil(t, state)
	assert.Equal(t, 0, state.GetRequestCount())
	assert.Equal(t, 0, state.GetPendingUpdates())
	assert.Equal(t, sequenceNumber, state.GetSequenceNumber())
	assert.Equal(t, cutoff, state.GetIdempotencyCutoff())
	assert.Equal(t, metadata, state.GetEntityMetadata())
	assert.NotNil(t, state.GetProcessedRequests())
	assert.Len(t, state.GetProcessedRequests(), 0)
}

func TestWorkflowState_ShouldContinueAsNew(t *testing.T) {
	tests := []struct {
		name              string
		requestCount      int
		threshold         int
		temporalSuggested bool
		expected          bool
	}{
		{
			name:         "should continue when threshold reached",
			requestCount: 1000,
			threshold:    1000,
			expected:     true,
		},
		{
			name:              "should continue when temporal suggests",
			requestCount:      500,
			threshold:         1000,
			temporalSuggested: true,
			expected:          true,
		},
		{
			name:         "should not continue below threshold",
			requestCount: 500,
			threshold:    1000,
			expected:     false,
		},
		{
			name:         "should continue when exceeding threshold",
			requestCount: 1500,
			threshold:    1000,
			expected:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := &WorkflowState{
				requestCount:      tt.requestCount,
				requestsBeforeCAN: tt.threshold,
			}

			mockCtx := NewMockWorkflowContext()
			mockCtx.SetContinueAsNewSuggested(tt.temporalSuggested)

			result := state.ShouldContinueAsNew(mockCtx)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestWorkflowState_IncrementSequenceNumber(t *testing.T) {
	state := &WorkflowState{sequenceNumber: 5}

	result := state.IncrementSequenceNumber()

	assert.Equal(t, int64(6), result)
	assert.Equal(t, int64(6), state.GetSequenceNumber())
}

func TestWorkflowState_RequestCountOperations(t *testing.T) {
	state := NewWorkflowState(time.Now())

	assert.Equal(t, 0, state.GetRequestCount())

	state.IncrementRequestCount()
	assert.Equal(t, 1, state.GetRequestCount())

	state.IncrementRequestCount()
	assert.Equal(t, 2, state.GetRequestCount())
}

func TestWorkflowState_PendingUpdatesOperations(t *testing.T) {
	state := NewWorkflowState(time.Now())

	assert.Equal(t, 0, state.GetPendingUpdates())

	state.IncrementPendingUpdates()
	assert.Equal(t, 1, state.GetPendingUpdates())

	state.IncrementPendingUpdates()
	assert.Equal(t, 2, state.GetPendingUpdates())

	state.DecrementPendingUpdates()
	assert.Equal(t, 1, state.GetPendingUpdates())

	state.DecrementPendingUpdates()
	assert.Equal(t, 0, state.GetPendingUpdates())
}

func TestWorkflowState_EntityMetadataOperations(t *testing.T) {
	state := NewWorkflowState(time.Now())

	assert.Nil(t, state.GetEntityMetadata())

	metadata := &entityv1.EntityMetadata{
		EntityId:   "test-entity",
		EntityType: "test.Entity",
		OrgId:      "test-org",
		CreatedAt:  timestamppb.Now(),
	}

	state.SetEntityMetadata(metadata)
	assert.Equal(t, metadata, state.GetEntityMetadata())
}

// Test IdempotencyChecker

func TestNewIdempotencyChecker(t *testing.T) {
	cutoff := time.Now()
	checker := NewIdempotencyChecker(cutoff)

	assert.NotNil(t, checker)
	assert.Equal(t, cutoff, checker.GetCutoff())
	assert.NotNil(t, checker.GetProcessedRequests())
	assert.Len(t, checker.GetProcessedRequests(), 0)
}

func TestIdempotencyChecker_IsRequestProcessed(t *testing.T) {
	cutoff := time.Now().Add(-1 * time.Hour)

	tests := []struct {
		name        string
		key         string
		requestTime time.Time
		setup       func(*IdempotencyChecker)
		expected    bool
	}{
		{
			name:        "empty key should not be processed",
			key:         "",
			requestTime: cutoff.Add(30 * time.Minute),
			expected:    false,
		},
		{
			name:        "request before cutoff should be processed",
			key:         "test-key",
			requestTime: cutoff.Add(-30 * time.Minute),
			expected:    true,
		},
		{
			name:        "request at cutoff should be processed",
			key:         "test-key",
			requestTime: cutoff,
			expected:    true,
		},
		{
			name:        "new request after cutoff should not be processed",
			key:         "new-key",
			requestTime: cutoff.Add(30 * time.Minute),
			expected:    false,
		},
		{
			name:        "duplicate request should be processed",
			key:         "duplicate-key",
			requestTime: cutoff.Add(30 * time.Minute),
			setup: func(ic *IdempotencyChecker) {
				ic.processedRequests["duplicate-key"] = cutoff.Add(15 * time.Minute)
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset checker for each test
			checker := NewIdempotencyChecker(cutoff)
			if tt.setup != nil {
				tt.setup(checker)
			}

			mockCtx := NewMockWorkflowContext()
			result := checker.IsRequestProcessed(mockCtx, tt.key, tt.requestTime)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestIdempotencyChecker_MarkRequestProcessed(t *testing.T) {
	cutoff := time.Now()
	checker := NewIdempotencyChecker(cutoff)
	mockCtx := NewMockWorkflowContext()
	testTime := time.Now().Add(1 * time.Hour)
	mockCtx.SetTime(testTime)

	// Empty key should not be marked
	checker.MarkRequestProcessed(mockCtx, "")
	assert.Len(t, checker.GetProcessedRequests(), 0)

	// Valid key should be marked
	checker.MarkRequestProcessed(mockCtx, "test-key")
	processed := checker.GetProcessedRequests()
	assert.Len(t, processed, 1)
	assert.Equal(t, testTime, processed["test-key"])
}

func TestIdempotencyChecker_GetCutoffForContinuation(t *testing.T) {
	cutoff := time.Now().Add(-2 * time.Hour)
	checker := NewIdempotencyChecker(cutoff)
	mockCtx := NewMockWorkflowContext()

	// Empty processed requests should return zero time
	result := checker.GetCutoffForContinuation(mockCtx)
	assert.True(t, result.IsZero())

	// Add some processed requests
	time1 := time.Now().Add(-1 * time.Hour)
	time2 := time.Now().Add(-30 * time.Minute)
	time3 := time.Now().Add(-45 * time.Minute)

	checker.processedRequests["key1"] = time1
	checker.processedRequests["key2"] = time2  // This should be the newest
	checker.processedRequests["key3"] = time3

	result = checker.GetCutoffForContinuation(mockCtx)
	assert.Equal(t, time2, result)
} 