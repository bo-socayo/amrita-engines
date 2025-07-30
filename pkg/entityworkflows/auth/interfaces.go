package auth

import (
	"time"
	entityv1 "github.com/bo-socayo/amrita-engines/gen/entity/v1"
)

// Authorizer defines the interface for request authorization
type Authorizer interface {
	AuthorizeRequest(requestCtx *entityv1.RequestContext, entityMetadata *entityv1.EntityMetadata) error
	ValidateRequestContext(requestCtx *entityv1.RequestContext) error
}

// MetadataManager defines the interface for EntityMetadata lifecycle management
type MetadataManager interface {
	InitializeFromRequest(
		entityID, entityType string,
		requestCtx *entityv1.RequestContext,
		createdAt time.Time,
	) (*entityv1.EntityMetadata, error)
	ValidateMetadata(metadata *entityv1.EntityMetadata) error
	IsMetadataInitialized(metadata *entityv1.EntityMetadata) bool
} 