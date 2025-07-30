package auth

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	entityv1 "github.com/bo-socayo/amrita-engines/gen/entity/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Test helpers

func createTestRequestContext() *entityv1.RequestContext {
	return &entityv1.RequestContext{
		OrgId:       "test-org",
		TeamId:      "test-team",
		UserId:      "test-user",
		Tenant:      "test-tenant",
		Environment: "test",
	}
}

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

// Test RequestAuthorizer

func TestNewRequestAuthorizer(t *testing.T) {
	authorizer := NewRequestAuthorizer()
	assert.NotNil(t, authorizer)
}

func TestRequestAuthorizer_AuthorizeRequest(t *testing.T) {
	authorizer := NewRequestAuthorizer()

	tests := []struct {
		name           string
		requestCtx     *entityv1.RequestContext
		entityMetadata *entityv1.EntityMetadata
		expectError    bool
		errorContains  string
	}{
		{
			name:           "successful authorization with matching contexts",
			requestCtx:     createTestRequestContext(),
			entityMetadata: createTestEntityMetadata(),
			expectError:    false,
		},
		{
			name:           "nil request context",
			requestCtx:     nil,
			entityMetadata: createTestEntityMetadata(),
			expectError:    true,
			errorContains:  "request context cannot be nil",
		},
		{
			name:           "nil entity metadata",
			requestCtx:     createTestRequestContext(),
			entityMetadata: nil,
			expectError:    true,
			errorContains:  "entity metadata cannot be nil",
		},
		{
			name: "org ID mismatch",
			requestCtx: &entityv1.RequestContext{
				OrgId:       "different-org",
				TeamId:      "test-team",
				UserId:      "test-user",
				Tenant:      "test-tenant",
				Environment: "test",
			},
			entityMetadata: createTestEntityMetadata(),
			expectError:    true,
			errorContains:  "organization ID mismatch",
		},
		{
			name: "team ID mismatch",
			requestCtx: &entityv1.RequestContext{
				OrgId:       "test-org",
				TeamId:      "different-team",
				UserId:      "test-user",
				Tenant:      "test-tenant",
				Environment: "test",
			},
			entityMetadata: createTestEntityMetadata(),
			expectError:    true,
			errorContains:  "team ID mismatch",
		},
		{
			name: "user ID mismatch",
			requestCtx: &entityv1.RequestContext{
				OrgId:       "test-org",
				TeamId:      "test-team",
				UserId:      "different-user",
				Tenant:      "test-tenant",
				Environment: "test",
			},
			entityMetadata: createTestEntityMetadata(),
			expectError:    true,
			errorContains:  "user ID mismatch",
		},
		{
			name: "tenant mismatch",
			requestCtx: &entityv1.RequestContext{
				OrgId:       "test-org",
				TeamId:      "test-team",
				UserId:      "test-user",
				Tenant:      "different-tenant",
				Environment: "test",
			},
			entityMetadata: createTestEntityMetadata(),
			expectError:    true,
			errorContains:  "tenant mismatch",
		},
		{
			name: "environment mismatch",
			requestCtx: &entityv1.RequestContext{
				OrgId:       "test-org",
				TeamId:      "test-team",
				UserId:      "test-user",
				Tenant:      "test-tenant",
				Environment: "production",
			},
			entityMetadata: createTestEntityMetadata(),
			expectError:    true,
			errorContains:  "environment mismatch",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := authorizer.AuthorizeRequest(tt.requestCtx, tt.entityMetadata)

			if tt.expectError {
				assert.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestRequestAuthorizer_ValidateRequestContext(t *testing.T) {
	authorizer := NewRequestAuthorizer()

	tests := []struct {
		name          string
		requestCtx    *entityv1.RequestContext
		expectError   bool
		errorContains string
	}{
		{
			name:        "valid request context",
			requestCtx:  createTestRequestContext(),
			expectError: false,
		},
		{
			name:          "nil request context",
			requestCtx:    nil,
			expectError:   true,
			errorContains: "request context cannot be nil",
		},
		{
			name: "missing org ID",
			requestCtx: &entityv1.RequestContext{
				OrgId:       "",
				TeamId:      "test-team",
				UserId:      "test-user",
				Tenant:      "test-tenant",
				Environment: "test",
			},
			expectError:   true,
			errorContains: "org ID is required",
		},
		{
			name: "missing tenant",
			requestCtx: &entityv1.RequestContext{
				OrgId:       "test-org",
				TeamId:      "test-team",
				UserId:      "test-user",
				Tenant:      "",
				Environment: "test",
			},
			expectError:   true,
			errorContains: "tenant is required",
		},
		{
			name: "missing environment",
			requestCtx: &entityv1.RequestContext{
				OrgId:       "test-org",
				TeamId:      "test-team",
				UserId:      "test-user",
				Tenant:      "test-tenant",
				Environment: "",
			},
			expectError:   true,
			errorContains: "environment is required",
		},
		{
			name: "empty team and user ID should be valid",
			requestCtx: &entityv1.RequestContext{
				OrgId:       "test-org",
				TeamId:      "",
				UserId:      "",
				Tenant:      "test-tenant",
				Environment: "test",
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := authorizer.ValidateRequestContext(tt.requestCtx)

			if tt.expectError {
				assert.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestAuthorizeRequestLegacy(t *testing.T) {
	requestCtx := createTestRequestContext()
	entityMetadata := createTestEntityMetadata()

	// Test successful authorization
	result := AuthorizeRequestLegacy(requestCtx, entityMetadata)
	assert.True(t, result)

	// Test failed authorization
	badRequestCtx := &entityv1.RequestContext{
		OrgId:       "different-org",
		TeamId:      "test-team",
		UserId:      "test-user",
		Tenant:      "test-tenant",
		Environment: "test",
	}
	result = AuthorizeRequestLegacy(badRequestCtx, entityMetadata)
	assert.False(t, result)
}

// Test EntityMetadataManager

func TestNewEntityMetadataManager(t *testing.T) {
	manager := NewEntityMetadataManager()
	assert.NotNil(t, manager)
}

func TestEntityMetadataManager_InitializeFromRequest(t *testing.T) {
	manager := NewEntityMetadataManager()
	createdAt := time.Now()

	tests := []struct {
		name          string
		entityID      string
		entityType    string
		requestCtx    *entityv1.RequestContext
		expectError   bool
		errorContains string
	}{
		{
			name:        "successful initialization",
			entityID:    "test-entity",
			entityType:  "test.Entity",
			requestCtx:  createTestRequestContext(),
			expectError: false,
		},
		{
			name:          "nil request context",
			entityID:      "test-entity",
			entityType:    "test.Entity",
			requestCtx:    nil,
			expectError:   true,
			errorContains: "request context cannot be nil",
		},
		{
			name:          "empty entity ID",
			entityID:      "",
			entityType:    "test.Entity",
			requestCtx:    createTestRequestContext(),
			expectError:   true,
			errorContains: "entity ID cannot be empty",
		},
		{
			name:          "empty entity type",
			entityID:      "test-entity",
			entityType:    "",
			requestCtx:    createTestRequestContext(),
			expectError:   true,
			errorContains: "entity type cannot be empty",
		},
		{
			name:       "missing org ID in request context",
			entityID:   "test-entity",
			entityType: "test.Entity",
			requestCtx: &entityv1.RequestContext{
				OrgId:       "",
				TeamId:      "test-team",
				UserId:      "test-user",
				Tenant:      "test-tenant",
				Environment: "test",
			},
			expectError:   true,
			errorContains: "org ID is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			metadata, err := manager.InitializeFromRequest(tt.entityID, tt.entityType, tt.requestCtx, createdAt)

			if tt.expectError {
				assert.Error(t, err)
				assert.Nil(t, metadata)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, metadata)
				assert.Equal(t, tt.entityID, metadata.EntityId)
				assert.Equal(t, tt.entityType, metadata.EntityType)
				assert.Equal(t, tt.requestCtx.OrgId, metadata.OrgId)
				assert.Equal(t, tt.requestCtx.TeamId, metadata.TeamId)
				assert.Equal(t, tt.requestCtx.UserId, metadata.UserId)
				assert.Equal(t, tt.requestCtx.Tenant, metadata.Tenant)
				assert.Equal(t, tt.requestCtx.Environment, metadata.Environment)
				assert.NotNil(t, metadata.CreatedAt)
			}
		})
	}
}

func TestEntityMetadataManager_ValidateMetadata(t *testing.T) {
	manager := NewEntityMetadataManager()

	tests := []struct {
		name          string
		metadata      *entityv1.EntityMetadata
		expectError   bool
		errorContains string
	}{
		{
			name:        "valid metadata",
			metadata:    createTestEntityMetadata(),
			expectError: false,
		},
		{
			name:          "nil metadata",
			metadata:      nil,
			expectError:   true,
			errorContains: "entity metadata cannot be nil",
		},
		{
			name: "empty entity ID",
			metadata: &entityv1.EntityMetadata{
				EntityId:    "",
				EntityType:  "test.Entity",
				OrgId:       "test-org",
				Tenant:      "test-tenant",
				Environment: "test",
				CreatedAt:   timestamppb.Now(),
			},
			expectError:   true,
			errorContains: "entity ID cannot be empty",
		},
		{
			name: "missing created at",
			metadata: &entityv1.EntityMetadata{
				EntityId:    "test-entity",
				EntityType:  "test.Entity",
				OrgId:       "test-org",
				Tenant:      "test-tenant",
				Environment: "test",
				CreatedAt:   nil,
			},
			expectError:   true,
			errorContains: "created at timestamp cannot be nil",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := manager.ValidateMetadata(tt.metadata)

			if tt.expectError {
				assert.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestEntityMetadataManager_IsMetadataInitialized(t *testing.T) {
	manager := NewEntityMetadataManager()

	validMetadata := createTestEntityMetadata()
	assert.True(t, manager.IsMetadataInitialized(validMetadata))

	invalidMetadata := &entityv1.EntityMetadata{
		EntityId: "", // Invalid - empty
	}
	assert.False(t, manager.IsMetadataInitialized(invalidMetadata))

	assert.False(t, manager.IsMetadataInitialized(nil))
} 