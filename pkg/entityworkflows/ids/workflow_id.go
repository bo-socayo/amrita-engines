package ids

import (
	"fmt"
	"strings"

	entityv1 "github.com/bo-socayo/amrita-engines/gen/entity/v1"
)

// EntityIDHandler handles standard business entity workflow IDs
type EntityIDHandler struct{}

// NewEntityIDHandler creates a new entity ID handler
func NewEntityIDHandler() *EntityIDHandler {
	return &EntityIDHandler{}
}

// BuildWorkflowID builds a workflow ID from entity metadata
func (h *EntityIDHandler) BuildWorkflowID(metadata *entityv1.EntityMetadata) string {
	return fmt.Sprintf("%s-%s", metadata.EntityType, metadata.EntityId)
}

// ParseWorkflowID parses a workflow ID to extract entity type and entity ID
func (h *EntityIDHandler) ParseWorkflowID(workflowID string) (entityType, entityID string, err error) {
	parts := strings.SplitN(workflowID, "-", 2)
	if len(parts) < 2 {
		return "", "", fmt.Errorf("invalid entity workflow ID format: %s", workflowID)
	}
	return parts[0], parts[1], nil
}

// ExtractContextFromMetadata extracts context information from entity metadata
func (h *EntityIDHandler) ExtractContextFromMetadata(metadata *entityv1.EntityMetadata) map[string]string {
	return map[string]string{
		"entity_type": metadata.EntityType,
		"entity_id":   metadata.EntityId,
		"org_id":      metadata.OrgId,
		"team_id":     metadata.TeamId,
		"user_id":     metadata.UserId,
		"environment": metadata.Environment,
		"tenant":      metadata.Tenant,
	}
} 