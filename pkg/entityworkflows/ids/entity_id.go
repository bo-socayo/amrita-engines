package ids

import (
	"fmt"
	"strings"

	"google.golang.org/protobuf/proto"
)

// GetEntityTypeFromProtobuf derives the entity type from a protobuf message
func GetEntityTypeFromProtobuf[T proto.Message]() string {
	instance := newInstance[T]()
	descriptor := instance.ProtoReflect().Descriptor()
	return string(descriptor.FullName())
}

// BuildEntityWorkflowIDFromProtobuf builds a workflow ID using protobuf type information
func BuildEntityWorkflowIDFromProtobuf[T proto.Message](entityID string) string {
	entityType := GetEntityTypeFromProtobuf[T]()
	return fmt.Sprintf("%s-%s", entityType, entityID)
}

// ParseEntityWorkflowID parses an entity workflow ID to extract type and ID
func ParseEntityWorkflowID(workflowID string) (entityType, entityID string, err error) {
	// This is a simple implementation - you might want more sophisticated parsing
	// for complex entity types with dots/hyphens
	parts := strings.SplitN(workflowID, "-", 2)
	if len(parts) < 2 {
		return "", "", fmt.Errorf("invalid entity workflow ID format: %s", workflowID)
	}
	return parts[0], parts[1], nil
}

// newInstance creates a new instance of type T for protobuf operations
func newInstance[T any]() T {
	var zero T
	return zero
} 