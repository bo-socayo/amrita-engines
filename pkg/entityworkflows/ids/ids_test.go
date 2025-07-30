package ids

import (
	"testing"

	"github.com/stretchr/testify/assert"
	entityv1 "github.com/bo-socayo/amrita-engines/gen/entity/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

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

func TestNewEntityIDHandler(t *testing.T) {
	handler := NewEntityIDHandler()
	assert.NotNil(t, handler)
}

func TestEntityIDHandler_BuildWorkflowID(t *testing.T) {
	handler := NewEntityIDHandler()
	metadata := createTestEntityMetadata()

	workflowID := handler.BuildWorkflowID(metadata)
	expected := "test.Entity-test-entity"

	assert.Equal(t, expected, workflowID)
}

func TestEntityIDHandler_ParseWorkflowID(t *testing.T) {
	handler := NewEntityIDHandler()

	tests := []struct {
		name         string
		workflowID   string
		expectedType string
		expectedID   string
		expectError  bool
	}{
		{
			name:         "valid workflow ID",
			workflowID:   "test.Entity-test-entity",
			expectedType: "test.Entity",
			expectedID:   "test-entity",
			expectError:  false,
		},
		{
			name:        "invalid workflow ID - no separator",
			workflowID:  "testEntity",
			expectError: true,
		},
		{
			name:        "invalid workflow ID - empty",
			workflowID:  "",
			expectError: true,
		},
		{
			name:         "workflow ID with multiple hyphens",
			workflowID:   "test.Entity-test-entity-with-hyphens",
			expectedType: "test.Entity",
			expectedID:   "test-entity-with-hyphens",
			expectError:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entityType, entityID, err := handler.ParseWorkflowID(tt.workflowID)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expectedType, entityType)
				assert.Equal(t, tt.expectedID, entityID)
			}
		})
	}
}

func TestEntityIDHandler_ExtractContextFromMetadata(t *testing.T) {
	handler := NewEntityIDHandler()
	metadata := createTestEntityMetadata()

	context := handler.ExtractContextFromMetadata(metadata)

	expected := map[string]string{
		"entity_type": "test.Entity",
		"entity_id":   "test-entity",
		"org_id":      "test-org",
		"team_id":     "test-team",
		"user_id":     "test-user",
		"environment": "test",
		"tenant":      "test-tenant",
	}

	assert.Equal(t, expected, context)
}

func TestParseEntityWorkflowID(t *testing.T) {
	tests := []struct {
		name         string
		workflowID   string
		expectedType string
		expectedID   string
		expectError  bool
	}{
		{
			name:         "valid workflow ID",
			workflowID:   "test.Entity-test-entity",
			expectedType: "test.Entity",
			expectedID:   "test-entity",
			expectError:  false,
		},
		{
			name:        "invalid workflow ID",
			workflowID:  "invalid",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entityType, entityID, err := ParseEntityWorkflowID(tt.workflowID)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expectedType, entityType)
				assert.Equal(t, tt.expectedID, entityID)
			}
		})
	}
}

func TestGetEntityTypeFromProtobuf(t *testing.T) {
	// Using timestamppb.Timestamp as a known protobuf type
	entityType := GetEntityTypeFromProtobuf[*timestamppb.Timestamp]()
	assert.Equal(t, "google.protobuf.Timestamp", entityType)
}

func TestBuildEntityWorkflowIDFromProtobuf(t *testing.T) {
	workflowID := BuildEntityWorkflowIDFromProtobuf[*timestamppb.Timestamp]("test-entity")
	expected := "google.protobuf.Timestamp-test-entity"
	assert.Equal(t, expected, workflowID)
} 