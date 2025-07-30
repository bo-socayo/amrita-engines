package handlers

import (
	"testing"

	"github.com/stretchr/testify/assert"
	entityv1 "github.com/bo-socayo/amrita-engines/gen/entity/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

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