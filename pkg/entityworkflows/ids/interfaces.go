package ids

import (
	entityv1 "github.com/bo-socayo/amrita-engines/gen/entity/v1"
)

// WorkflowIDHandler provides pluggable ID handling for different entity types
type WorkflowIDHandler interface {
	BuildWorkflowID(metadata *entityv1.EntityMetadata) string
	ParseWorkflowID(workflowID string) (entityType, entityID string, err error)
	ExtractContextFromMetadata(metadata *entityv1.EntityMetadata) map[string]string
} 