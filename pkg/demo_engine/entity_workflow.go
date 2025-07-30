package demo_engine

import (
	"fmt"
	"strings"

	"go.temporal.io/sdk/workflow"

	demov1 "github.com/bo-socayo/amrita-engines/gen/engines/demo/v1"
	entityv1 "github.com/bo-socayo/amrita-engines/gen/entity/v1"
	"github.com/bo-socayo/amrita-engines/pkg/engines"
	"github.com/bo-socayo/amrita-engines/pkg/entityworkflows"
)

// DemoWorkflowIDHandler handles workflow ID generation for demo engine
type DemoWorkflowIDHandler struct{}

// BuildWorkflowID generates a workflow ID for the demo engine entity workflow
func (h *DemoWorkflowIDHandler) BuildWorkflowID(metadata *entityv1.EntityMetadata) string {
	return "demo-engine-" + metadata.EntityId
}

// ParseWorkflowID parses a workflow ID to extract entity type and ID
func (h *DemoWorkflowIDHandler) ParseWorkflowID(workflowID string) (entityType, entityID string, err error) {
	if !strings.HasPrefix(workflowID, "demo-engine-") {
		return "", "", fmt.Errorf("invalid demo engine workflow ID format: %s", workflowID)
	}
	entityID = strings.TrimPrefix(workflowID, "demo-engine-")
	return "demo-engine", entityID, nil
}

// ExtractContextFromMetadata extracts context information from metadata
func (h *DemoWorkflowIDHandler) ExtractContextFromMetadata(metadata *entityv1.EntityMetadata) map[string]string {
	return map[string]string{
		"entity_type": "demo-engine",
		"entity_id":   metadata.EntityId,
		"org_id":      metadata.OrgId,
		"team_id":     metadata.TeamId,
		"user_id":     metadata.UserId,
		"environment": metadata.Environment,
		"tenant":      metadata.Tenant,
	}
}

// DemoEntityWorkflow uses the generic entity workflow with demo engine
func DemoEntityWorkflow(
	ctx workflow.Context,
	params entityworkflows.EntityWorkflowParams[*demov1.DemoEngineState],
) error {
	// Get type names from protobuf descriptors
	stateDescriptor := (&demov1.DemoEngineState{}).ProtoReflect().Descriptor()
	signalDescriptor := (&demov1.DemoEngineSignal{}).ProtoReflect().Descriptor()
	transitionDescriptor := (&demov1.DemoEngineTransitionInfo{}).ProtoReflect().Descriptor()

	// Create demo engine with custom compression
	config := engines.BaseEngineConfig[*demov1.DemoEngineState, *demov1.DemoEngineSignal, *demov1.DemoEngineTransitionInfo]{
		EngineName:           EngineName,
		BusinessLogicVersion: BusinessLogicVersion,
		StateTypeName:        string(stateDescriptor.Name()),
		EventTypeName:        string(signalDescriptor.Name()),
		TransitionTypeName:   string(transitionDescriptor.Name()),
		Processor:            ProcessSignal,
		Defaults:             ApplyBusinessDefaults,
		Compress:             CompressState, // Use our custom compression function
	}
	
	engine := engines.NewBaseEngine(config)
	idHandler := &DemoWorkflowIDHandler{}

	// Use the generic entity workflow - it now handles continue-as-new with correct workflow name
	return entityworkflows.EntityWorkflow[*demov1.DemoEngineState, *demov1.DemoEngineSignal, *demov1.DemoEngineTransitionInfo](
		ctx, 
		params, 
		engine, 
		idHandler,
	)
} 