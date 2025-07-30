package utils

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestNewInstance_PointerType(t *testing.T) {
	// Test with pointer type (protobuf message)
	instance := NewInstance[*timestamppb.Timestamp]()
	assert.NotNil(t, instance)
	assert.IsType(t, &timestamppb.Timestamp{}, instance)
}

func TestNewInstance_NonPointerType(t *testing.T) {
	// Test with non-pointer type
	instance := NewInstance[int]()
	assert.Equal(t, 0, instance)
}

func TestMin(t *testing.T) {
	tests := []struct {
		a, b, expected int
	}{
		{5, 3, 3},
		{3, 5, 3},
		{5, 5, 5},
		{0, 1, 0},
		{-1, 1, -1},
	}

	for _, tt := range tests {
		result := Min(tt.a, tt.b)
		assert.Equal(t, tt.expected, result)
	}
} 