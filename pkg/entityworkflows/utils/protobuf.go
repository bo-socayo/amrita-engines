package utils

import (
	"reflect"

	"google.golang.org/protobuf/proto"
)

// NewInstance creates a new instance of type T for unmarshaling
func NewInstance[T any]() T {
	var zero T
	t := reflect.TypeOf(zero)
	
	// If T is a pointer type, create a new instance of the pointed-to type
	if t != nil && t.Kind() == reflect.Ptr {
		// Create new instance of the element type
		elem := reflect.New(t.Elem())
		instance := elem.Interface().(T)
		
		// If it's a protobuf message, ensure it's properly initialized
		if msg, ok := any(instance).(proto.Message); ok {
			proto.Reset(msg)
		}
		
		return instance
	}
	
	// For non-pointer types, return zero value
	return zero
} 