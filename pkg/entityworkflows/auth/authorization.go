package auth

import (
	"fmt"
	entityv1 "github.com/bo-socayo/amrita-engines/gen/entity/v1"
)

// RequestAuthorizer implements the Authorizer interface
type RequestAuthorizer struct{}

// NewRequestAuthorizer creates a new request authorizer
func NewRequestAuthorizer() *RequestAuthorizer {
	return &RequestAuthorizer{}
}

// AuthorizeRequest validates that the RequestContext matches the established EntityMetadata
func (a *RequestAuthorizer) AuthorizeRequest(requestCtx *entityv1.RequestContext, entityMetadata *entityv1.EntityMetadata) error {
	if requestCtx == nil {
		return fmt.Errorf("request context cannot be nil")
	}
	
	if entityMetadata == nil {
		return fmt.Errorf("entity metadata cannot be nil")
	}
	
	// Check all authorization fields must match
	if requestCtx.OrgId != entityMetadata.OrgId {
		return fmt.Errorf("organization ID mismatch: request=%s, entity=%s", requestCtx.OrgId, entityMetadata.OrgId)
	}
	
	if requestCtx.TeamId != entityMetadata.TeamId {
		return fmt.Errorf("team ID mismatch: request=%s, entity=%s", requestCtx.TeamId, entityMetadata.TeamId)
	}
	
	if requestCtx.UserId != entityMetadata.UserId {
		return fmt.Errorf("user ID mismatch: request=%s, entity=%s", requestCtx.UserId, entityMetadata.UserId)
	}
	
	if requestCtx.Tenant != entityMetadata.Tenant {
		return fmt.Errorf("tenant mismatch: request=%s, entity=%s", requestCtx.Tenant, entityMetadata.Tenant)
	}
	
	if requestCtx.Environment != entityMetadata.Environment {
		return fmt.Errorf("environment mismatch: request=%s, entity=%s", requestCtx.Environment, entityMetadata.Environment)
	}
	
	return nil
}

// ValidateRequestContext validates the request context has required fields
func (a *RequestAuthorizer) ValidateRequestContext(requestCtx *entityv1.RequestContext) error {
	if requestCtx == nil {
		return fmt.Errorf("request context cannot be nil")
	}
	
	if requestCtx.OrgId == "" {
		return fmt.Errorf("org ID is required")
	}
	
	if requestCtx.Tenant == "" {
		return fmt.Errorf("tenant is required")
	}
	
	if requestCtx.Environment == "" {
		return fmt.Errorf("environment is required")
	}
	
	// Note: TeamId and UserId might be optional depending on use case
	
	return nil
}

// AuthorizeRequestLegacy provides backward compatibility with the original boolean return
func AuthorizeRequestLegacy(requestCtx *entityv1.RequestContext, entityMetadata *entityv1.EntityMetadata) bool {
	authorizer := NewRequestAuthorizer()
	err := authorizer.AuthorizeRequest(requestCtx, entityMetadata)
	return err == nil
} 