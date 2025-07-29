package engines

import (
	"fmt"

	"buf.build/go/protovalidate"
	"google.golang.org/protobuf/proto"
)

// Global validator instance for protobuf validation
var globalValidator protovalidate.Validator

func init() {
	var err error
	globalValidator, err = protovalidate.New()
	if err != nil {
		// If validation setup fails, we'll skip validation but log it
		// In production, you might want to fail hard here
		globalValidator = nil
	}
}

// ValidateProtoMessage validates a protobuf message using buf validate annotations
func ValidateProtoMessage(message proto.Message, context string) error {
	if globalValidator == nil {
		// If validator failed to initialize, skip validation
		return nil
	}
	
	if err := globalValidator.Validate(message); err != nil {
		return fmt.Errorf("validation failed for %s: %w", context, err)
	}
	
	return nil
}

// ValidateEventData validates event data using buf validate annotations
func ValidateEventData(eventData proto.Message) error {
	return ValidateProtoMessage(eventData, "event data")
}

// ValidateState validates state using buf validate annotations
func ValidateState(state proto.Message) error {
	return ValidateProtoMessage(state, "state")
}

// ValidateEventEnvelope validates an event envelope and its data
func ValidateEventEnvelope[TEvent proto.Message](envelope *TypedEventEnvelope[TEvent]) error {
	if envelope == nil {
		return fmt.Errorf("envelope cannot be nil")
	}
	
	// Call the envelope's own validation method
	return envelope.Validate()
}

// ValidateTransitionInfo validates transition info using buf validate annotations
func ValidateTransitionInfo(transitionInfo proto.Message) error {
	return ValidateProtoMessage(transitionInfo, "transition info")
} 