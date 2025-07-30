package auth

import (
	"fmt"
	"time"
	
	entityv1 "github.com/bo-socayo/amrita-engines/gen/entity/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// EntityMetadataManager implements the MetadataManager interface
type EntityMetadataManager struct{}

// NewEntityMetadataManager creates a new entity metadata manager
func NewEntityMetadataManager() *EntityMetadataManager {
	return &EntityMetadataManager{}
}

// InitializeFromRequest creates EntityMetadata from first request
func (mm *EntityMetadataManager) InitializeFromRequest(
	entityID, entityType string,
	requestCtx *entityv1.RequestContext,
	createdAt time.Time,
) (*entityv1.EntityMetadata, error) {
	if requestCtx == nil {
		return nil, fmt.Errorf("request context cannot be nil for metadata initialization")
	}
	
	if entityID == "" {
		return nil, fmt.Errorf("entity ID cannot be empty")
	}
	
	if entityType == "" {
		return nil, fmt.Errorf("entity type cannot be empty")
	}
	
	// Validate required fields in request context
	if requestCtx.OrgId == "" {
		return nil, fmt.Errorf("org ID is required for metadata initialization")
	}
	
	if requestCtx.Tenant == "" {
		return nil, fmt.Errorf("tenant is required for metadata initialization")
	}
	
	if requestCtx.Environment == "" {
		return nil, fmt.Errorf("environment is required for metadata initialization")
	}
	
	entityMetadata := &entityv1.EntityMetadata{
		EntityId:    entityID,
		EntityType:  entityType,
		OrgId:       requestCtx.OrgId,
		TeamId:      requestCtx.TeamId,
		UserId:      requestCtx.UserId,
		Environment: requestCtx.Environment,
		Tenant:      requestCtx.Tenant,
		CreatedAt:   timestamppb.New(createdAt),
	}
	
	// Validate the created metadata
	err := mm.ValidateMetadata(entityMetadata)
	if err != nil {
		return nil, fmt.Errorf("created metadata failed validation: %w", err)
	}
	
	return entityMetadata, nil
}

// ValidateMetadata validates entity metadata has required fields
func (mm *EntityMetadataManager) ValidateMetadata(metadata *entityv1.EntityMetadata) error {
	if metadata == nil {
		return fmt.Errorf("entity metadata cannot be nil")
	}
	
	if metadata.EntityId == "" {
		return fmt.Errorf("entity ID cannot be empty")
	}
	
	if metadata.EntityType == "" {
		return fmt.Errorf("entity type cannot be empty")
	}
	
	if metadata.OrgId == "" {
		return fmt.Errorf("org ID cannot be empty")
	}
	
	if metadata.Tenant == "" {
		return fmt.Errorf("tenant cannot be empty")
	}
	
	if metadata.Environment == "" {
		return fmt.Errorf("environment cannot be empty")
	}
	
	if metadata.CreatedAt == nil {
		return fmt.Errorf("created at timestamp cannot be nil")
	}
	
	// Note: TeamId and UserId might be optional depending on use case
	
	return nil
}

// IsMetadataInitialized checks if metadata is properly initialized
func (mm *EntityMetadataManager) IsMetadataInitialized(metadata *entityv1.EntityMetadata) bool {
	return mm.ValidateMetadata(metadata) == nil
} 