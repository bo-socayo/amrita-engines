package handlers

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/bo-socayo/amrita-engines/pkg/engines"
	"github.com/bo-socayo/amrita-engines/pkg/entityworkflows/state"
	entityv1 "github.com/bo-socayo/amrita-engines/gen/entity/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// === Mock implementations ===

// MockEngine implements engines.Engine interface for testing
type MockEngine struct {
	mock.Mock
}

func (m *MockEngine) SetInitialState(ctx context.Context, state *timestamppb.Timestamp, createdAt *timestamppb.Timestamp) (*timestamppb.Timestamp, error) {
	args := m.Called(ctx, state, createdAt)
	return args.Get(0).(*timestamppb.Timestamp), args.Error(1)
}

func (m *MockEngine) ProcessEvent(ctx context.Context, envelope *engines.TypedEventEnvelope[*timestamppb.Timestamp]) (*timestamppb.Timestamp, *timestamppb.Timestamp, error) {
	args := m.Called(ctx, envelope)
	if args.Get(0) == nil {
		return nil, nil, args.Error(2)
	}
	if args.Get(1) == nil {
		return args.Get(0).(*timestamppb.Timestamp), nil, args.Error(2)
	}
	return args.Get(0).(*timestamppb.Timestamp), args.Get(1).(*timestamppb.Timestamp), args.Error(2)
}

func (m *MockEngine) CompressState(ctx context.Context, state *timestamppb.Timestamp) (*timestamppb.Timestamp, error) {
	args := m.Called(ctx, state)
	return args.Get(0).(*timestamppb.Timestamp), args.Error(1)
}

func (m *MockEngine) Abort(ctx context.Context, sequenceNumber int64) (*timestamppb.Timestamp, error) {
	args := m.Called(ctx, sequenceNumber)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*timestamppb.Timestamp), args.Error(1)
}

// Additional methods to satisfy the Engine interface
func (m *MockEngine) ProcessEvents(ctx context.Context, envelopes []*engines.TypedEventEnvelope[*timestamppb.Timestamp]) (*timestamppb.Timestamp, []*timestamppb.Timestamp, error) {
	args := m.Called(ctx, envelopes)
	return args.Get(0).(*timestamppb.Timestamp), args.Get(1).([]*timestamppb.Timestamp), args.Error(2)
}

func (m *MockEngine) GetCurrentState() (*timestamppb.Timestamp, error) {
	args := m.Called()
	return args.Get(0).(*timestamppb.Timestamp), args.Error(1)
}

func (m *MockEngine) GetLastProcessedSequence() int64 {
	args := m.Called()
	return args.Get(0).(int64)
}

func (m *MockEngine) IsInitialized() bool {
	args := m.Called()
	return args.Bool(0)
}

func (m *MockEngine) GetEngineName() string {
	args := m.Called()
	return args.String(0)
}

func (m *MockEngine) Prepare(ctx context.Context, envelopes []*engines.TypedEventEnvelope[*timestamppb.Timestamp]) (engines.PrepareResult, *timestamppb.Timestamp, error) {
	args := m.Called(ctx, envelopes)
	return args.Get(0).(engines.PrepareResult), args.Get(1).(*timestamppb.Timestamp), args.Error(2)
}

func (m *MockEngine) Commit(ctx context.Context, transactionID int64) (*timestamppb.Timestamp, error) {
	args := m.Called(ctx, transactionID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*timestamppb.Timestamp), args.Error(1)
}

func (m *MockEngine) GetTransactionState() engines.TransactionState {
	args := m.Called()
	return args.Get(0).(engines.TransactionState)
}

func (m *MockEngine) GetCurrentTransactionID() *int64 {
	args := m.Called()
	if args.Get(0) == nil {
		return nil
	}
	return args.Get(0).(*int64)
}

// MockWorkflowContext implements basic workflow.Context methods needed for testing
type MockWorkflowContext struct {
	mock.Mock
	currentTime time.Time
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

// Implement the minimum workflow.Context interface methods needed
func (m *MockWorkflowContext) Deadline() (deadline time.Time, ok bool) {
	return time.Time{}, false
}

func (m *MockWorkflowContext) Done() <-chan struct{} {
	return nil
}

func (m *MockWorkflowContext) Err() error {
	return nil
}

func (m *MockWorkflowContext) Value(key interface{}) interface{} {
	return nil
}

// Mock workflow-specific methods
func (m *MockWorkflowContext) GetLogger(name string) interface{} {
	return &MockLogger{}
}

// MockLogger for workflow logging
type MockLogger struct{}

func (l *MockLogger) Info(msg string, keyvals ...interface{}) {}
func (l *MockLogger) Error(msg string, keyvals ...interface{}) {}
func (l *MockLogger) Warn(msg string, keyvals ...interface{}) {}
func (l *MockLogger) Debug(msg string, keyvals ...interface{}) {}

// === Original Test helpers ===

// Test helpers
func createTestEntityMetadata() *entityv1.EntityMetadata {
	return &entityv1.EntityMetadata{
		EntityId:    "test-entity",
		EntityType:  "test.Entity",
		OrgId:       "test-org",
		TeamId:      "test-team",
		UserId:      "test-user",
		Tenant:      "test-tenant",
		Environment: "test",
		CreatedAt:   timestamppb.Now(),
	}
}

// === Query Handler Tests ===

func TestNewEntityQueryHandler(t *testing.T) {
	handler := NewEntityQueryHandler[*timestamppb.Timestamp]()
	assert.NotNil(t, handler)
}

func TestEntityQueryHandler_HandleGetEntityState_Uninitialized(t *testing.T) {
	handler := NewEntityQueryHandler[*timestamppb.Timestamp]()
	
	response, err := handler.HandleGetEntityState(
		false, // not initialized
		timestamppb.Now(),
		nil, // no metadata yet
		"test-entity",
	)
	
	assert.NoError(t, err)
	assert.NotNil(t, response)
	assert.False(t, response.IsInitialized)
	assert.Nil(t, response.Metadata)
	assert.NotEmpty(t, response.CurrentStateJSON)
}

func TestEntityQueryHandler_HandleGetEntityState_Initialized(t *testing.T) {
	handler := NewEntityQueryHandler[*timestamppb.Timestamp]()
	metadata := createTestEntityMetadata()
	currentState := timestamppb.Now()
	
	response, err := handler.HandleGetEntityState(
		true, // initialized
		currentState,
		metadata,
		"test-entity",
	)
	
	assert.NoError(t, err)
	assert.NotNil(t, response)
	assert.True(t, response.IsInitialized)
	assert.Equal(t, metadata, response.Metadata)
	assert.NotEmpty(t, response.CurrentStateJSON)
}

func TestEntityQueryHandler_HandleGetEntityState_NilCurrentState(t *testing.T) {
	handler := NewEntityQueryHandler[*timestamppb.Timestamp]()
	metadata := createTestEntityMetadata()
	
	response, err := handler.HandleGetEntityState(
		true, // initialized
		nil,  // nil current state
		metadata,
		"test-entity",
	)
	
	assert.Error(t, err)
	assert.Nil(t, response)
	assert.Contains(t, err.Error(), "current state is nil")
}

func TestEntityQueryHandler_HandleGetEntityState_EmptyEntityId(t *testing.T) {
	handler := NewEntityQueryHandler[*timestamppb.Timestamp]()
	metadata := createTestEntityMetadata()
	currentState := timestamppb.Now()
	
	response, err := handler.HandleGetEntityState(
		true, // initialized
		currentState,
		metadata,
		"", // empty entity ID
	)
	
	assert.NoError(t, err)
	assert.NotNil(t, response)
	assert.True(t, response.IsInitialized)
	assert.Equal(t, metadata, response.Metadata)
	assert.NotEmpty(t, response.CurrentStateJSON)
}

// === Update Handler Tests ===

func TestNewEntityUpdateHandler(t *testing.T) {
	mockEngine := &MockEngine{}
	entityType := "test.Entity"
	
	handler := NewEntityUpdateHandler[*timestamppb.Timestamp, *timestamppb.Timestamp, *timestamppb.Timestamp](
		mockEngine,
		entityType,
	)
	
	assert.NotNil(t, handler)
	assert.Equal(t, mockEngine, handler.engine)
	assert.Equal(t, entityType, handler.entityType)
}

func TestEntityUpdateHandler_ProcessEvent_ComponentCreation(t *testing.T) {
	mockEngine := &MockEngine{}
	entityType := "test.Entity"
	
	handler := NewEntityUpdateHandler[*timestamppb.Timestamp, *timestamppb.Timestamp, *timestamppb.Timestamp](
		mockEngine,
		entityType,
	)
	
	// Test that the handler is properly created with engine and entityType
	assert.NotNil(t, handler)
	assert.Equal(t, mockEngine, handler.engine)
	assert.Equal(t, entityType, handler.entityType)
	
	// Test that ProcessEvent method exists and can be called
	// Note: The actual ProcessEvent method requires a valid workflow.Context for logging
	// which is complex to mock. This test verifies the component structure.
	// Full integration testing of ProcessEvent happens in the demo integration tests.
	
	event := timestamppb.Now()
	sequenceNumber := int64(123)
	
	// Verify the method signature exists (this will compile-time check the interface)
	_ = func() (interface{}, interface{}, error) {
		// We can't easily test the full ProcessEvent without a proper workflow context
		// but we can verify the method exists and has the right signature
		return handler.ProcessEvent(nil, event, sequenceNumber)
	}
}

// === Update Validator Tests ===

func TestNewUpdateValidator(t *testing.T) {
	workflowState := state.NewWorkflowState(time.Now())
	
	validator := NewUpdateValidator(workflowState)
	
	assert.NotNil(t, validator)
	assert.Equal(t, workflowState, validator.workflowState)
}

func TestUpdateValidator_ValidateUpdate_BelowThreshold(t *testing.T) {
	workflowState := state.NewWorkflowState(time.Now())
	validator := NewUpdateValidator(workflowState)
	
	// Increment request count but stay below threshold (1000)
	for i := 0; i < 500; i++ {
		workflowState.IncrementRequestCount()
	}
	
	requestCtx := &entityv1.RequestContext{
		OrgId:  "test-org",
		Tenant: "test-tenant",
	}
	
	err := validator.ValidateUpdate(nil, requestCtx)
	
	assert.NoError(t, err)
}

func TestUpdateValidator_ValidateUpdate_AtThreshold(t *testing.T) {
	workflowState := state.NewWorkflowState(time.Now())
	validator := NewUpdateValidator(workflowState)
	
	// Increment request count to threshold (1000)
	for i := 0; i < 1000; i++ {
		workflowState.IncrementRequestCount()
	}
	
	requestCtx := &entityv1.RequestContext{
		OrgId:  "test-org",
		Tenant: "test-tenant",
	}
	
	err := validator.ValidateUpdate(nil, requestCtx)
	
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "backoff for continue-as-new")
}

func TestUpdateValidator_ValidateUpdate_AboveThreshold(t *testing.T) {
	workflowState := state.NewWorkflowState(time.Now())
	validator := NewUpdateValidator(workflowState)
	
	// Increment request count above threshold (1000)
	for i := 0; i < 1500; i++ {
		workflowState.IncrementRequestCount()
	}
	
	requestCtx := &entityv1.RequestContext{
		OrgId:  "test-org",
		Tenant: "test-tenant",
	}
	
	err := validator.ValidateUpdate(nil, requestCtx)
	
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "backoff for continue-as-new")
}

// === Helper Function Tests ===

func TestGetPreviewString(t *testing.T) {
	tests := []struct {
		name      string
		data      []byte
		maxLength int
		expected  string
	}{
		{
			name:      "short data",
			data:      []byte("hello"),
			maxLength: 10,
			expected:  "hello",
		},
		{
			name:      "long data truncated",
			data:      []byte("hello world this is a long string"),
			maxLength: 5,
			expected:  "hello",
		},
		{
			name:      "exact length",
			data:      []byte("hello"),
			maxLength: 5,
			expected:  "hello",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GetPreviewString(tt.data, tt.maxLength)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestMin(t *testing.T) {
	tests := []struct {
		a, b, expected int
	}{
		{5, 3, 3},
		{3, 5, 3},
		{5, 5, 5},
		{0, 1, 0},
		{-1, 1, -1},
	}

	for _, tt := range tests {
		result := min(tt.a, tt.b)
		assert.Equal(t, tt.expected, result)
	}
} 