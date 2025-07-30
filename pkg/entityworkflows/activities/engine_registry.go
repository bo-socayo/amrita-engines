package activities

import (
	"fmt"
	"sync"

	"github.com/bo-socayo/amrita-engines/pkg/engines"
	"google.golang.org/protobuf/proto"
)

// EngineFactory is a generic factory function for creating engines
type EngineFactory[TState, TEvent, TTransitionInfo proto.Message] func() engines.Engine[TState, TEvent, TTransitionInfo]

// engineRegistry holds all registered engine factories
var (
	engineRegistry = make(map[string]interface{})
	registryMutex  sync.RWMutex
)

// RegisterEngine registers an engine factory for a specific entity type
func RegisterEngine[TState, TEvent, TTransitionInfo proto.Message](
	entityType string,
	factory EngineFactory[TState, TEvent, TTransitionInfo],
) {
	registryMutex.Lock()
	defer registryMutex.Unlock()
	
	engineRegistry[entityType] = factory
}

// GetEngineForType retrieves and creates an engine instance for the given entity type
func GetEngineForType[TState, TEvent, TTransitionInfo proto.Message](
	entityType string,
) (engines.Engine[TState, TEvent, TTransitionInfo], error) {
	registryMutex.RLock()
	factoryInterface, exists := engineRegistry[entityType]
	registryMutex.RUnlock()
	
	if !exists {
		return nil, fmt.Errorf("no engine registered for entity type: %s", entityType)
	}
	
	// Type assert to the correct factory type
	factory, ok := factoryInterface.(EngineFactory[TState, TEvent, TTransitionInfo])
	if !ok {
		return nil, fmt.Errorf("engine factory type mismatch for entity type: %s", entityType)
	}
	
	// Create and return new engine instance
	return factory(), nil
}

// GetRegisteredEntityTypes returns a list of all registered entity types
func GetRegisteredEntityTypes() []string {
	registryMutex.RLock()
	defer registryMutex.RUnlock()
	
	types := make([]string, 0, len(engineRegistry))
	for entityType := range engineRegistry {
		types = append(types, entityType)
	}
	return types
}

// ClearRegistry clears all registered engines (useful for testing)
func ClearRegistry() {
	registryMutex.Lock()
	defer registryMutex.Unlock()
	
	engineRegistry = make(map[string]interface{})
} 