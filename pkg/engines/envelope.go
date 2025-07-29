package engines

import (
	"fmt"

	basev1 "github.com/bo-socayo/amrita-engines/gen/engines/base/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// EventEnvelope is an alias for the generated protobuf type
type EventEnvelope = basev1.EventEnvelope

// NewEventEnvelope creates a new event envelope with the given event data
func NewEventEnvelope[TEvent proto.Message](
	sequenceNumber int64,
	eventID string,
	timestamp *timestamppb.Timestamp,
	data TEvent,
	metadata proto.Message,
) (*EventEnvelope, error) {
	// Convert data to protobuf Any
	dataAny, err := anypb.New(data)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal event data: %w", err)
	}
	
	// Convert metadata to protobuf Any if provided
	var metadataAny *anypb.Any
	if metadata != nil {
		metadataAny, err = anypb.New(metadata)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal metadata: %w", err)
		}
	}
	
	envelope := &EventEnvelope{
		SequenceNumber: sequenceNumber,
		EventId:        eventID,
		Timestamp:      timestamp,
		Data:           dataAny,
		Metadata:       metadataAny,
	}
	
	if err := ValidateProtoMessage(envelope, "EventEnvelope"); err != nil {
		return nil, fmt.Errorf("invalid envelope: %w", err)
	}
	
	return envelope, nil
}

// UnmarshalEventData extracts the event data from the envelope into the provided message
func UnmarshalEventData(e *EventEnvelope, target proto.Message) error {
	if e.Data == nil {
		return fmt.Errorf("envelope data is nil")
	}
	
	return e.Data.UnmarshalTo(target)
}

// UnmarshalMetadata extracts the metadata from the envelope into the provided message
func UnmarshalMetadata(e *EventEnvelope, target proto.Message) error {
	if e.Metadata == nil {
		return fmt.Errorf("envelope metadata is nil")
	}
	
	return e.Metadata.UnmarshalTo(target)
}

// GetEventType returns the type URL of the event data
func GetEventType(e *EventEnvelope) string {
	if e.Data == nil {
		return ""
	}
	return e.Data.GetTypeUrl()
} 