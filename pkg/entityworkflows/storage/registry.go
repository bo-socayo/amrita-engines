package storage

import (
	"fmt"
	"log"
	"sync"
)

// StorageAdapterRegistry manages adapter instances per entity type
type StorageAdapterRegistry struct {
	adapters map[string]StorageAdapter
	mutex    sync.RWMutex
}

// NewStorageAdapterRegistry creates a new registry instance
func NewStorageAdapterRegistry() *StorageAdapterRegistry {
	return &StorageAdapterRegistry{
		adapters: make(map[string]StorageAdapter),
	}
}

// RegisterStorageAdapter registers an adapter for specific entity types
func (r *StorageAdapterRegistry) RegisterStorageAdapter(entityType string, adapter StorageAdapter) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	
	// Clean up previous adapter if exists
	if existing, exists := r.adapters[entityType]; exists && existing != nil {
		if err := existing.Close(); err != nil {
			log.Printf("Warning: failed to close existing storage adapter for %s: %v", entityType, err)
		}
	}
	
	r.adapters[entityType] = adapter
	if adapter != nil {
		log.Printf("Storage adapter registered for entity type: %s", entityType)
	} else {
		log.Printf("Storage adapter unregistered for entity type: %s", entityType)
	}
}

// GetStorageAdapter retrieves adapter for specific entity type
func (r *StorageAdapterRegistry) GetStorageAdapter(entityType string) StorageAdapter {
	r.mutex.RLock()
	defer r.mutex.RUnlock()
	return r.adapters[entityType]
}

// HasStorageAdapter checks if an adapter is registered for the entity type
func (r *StorageAdapterRegistry) HasStorageAdapter(entityType string) bool {
	r.mutex.RLock()
	defer r.mutex.RUnlock()
	adapter, exists := r.adapters[entityType]
	return exists && adapter != nil
}

// ListEntityTypes returns all entity types with registered adapters
func (r *StorageAdapterRegistry) ListEntityTypes() []string {
	r.mutex.RLock()
	defer r.mutex.RUnlock()
	
	var entityTypes []string
	for entityType, adapter := range r.adapters {
		if adapter != nil {
			entityTypes = append(entityTypes, entityType)
		}
	}
	return entityTypes
}

// CloseAll closes all registered adapters and clears the registry
func (r *StorageAdapterRegistry) CloseAll() error {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	
	var firstError error
	for entityType, adapter := range r.adapters {
		if adapter != nil {
			if err := adapter.Close(); err != nil {
				log.Printf("Error closing storage adapter for %s: %v", entityType, err)
				if firstError == nil {
					firstError = err
				}
			}
		}
	}
	
	// Clear the map
	r.adapters = make(map[string]StorageAdapter)
	return firstError
}

// Global registry instance (process-wide)
var globalStorageRegistry = NewStorageAdapterRegistry()

// Global functions for convenience

// RegisterStorageAdapter registers an adapter globally for an entity type
func RegisterStorageAdapter(entityType string, adapter StorageAdapter) {
	globalStorageRegistry.RegisterStorageAdapter(entityType, adapter)
}

// GetStorageAdapter retrieves the global adapter for an entity type
func GetStorageAdapter(entityType string) StorageAdapter {
	return globalStorageRegistry.GetStorageAdapter(entityType)
}

// HasStorageAdapter checks if a global adapter exists for an entity type
func HasStorageAdapter(entityType string) bool {
	return globalStorageRegistry.HasStorageAdapter(entityType)
}

// AdapterFactory is a function that creates a storage adapter
type AdapterFactory func(config StorageAdapterConfig) (StorageAdapter, error)

// Global registry of adapter factories
var adapterFactories = make(map[string]AdapterFactory)
var factoryMutex sync.RWMutex

// RegisterAdapterFactory registers a factory function for an adapter type
func RegisterAdapterFactory(adapterType string, factory AdapterFactory) {
	factoryMutex.Lock()
	defer factoryMutex.Unlock()
	adapterFactories[adapterType] = factory
}

// CreateStorageAdapter creates a storage adapter based on config
func CreateStorageAdapter(config StorageAdapterConfig) (StorageAdapter, error) {
	factoryMutex.RLock()
	factory, exists := adapterFactories[config.AdapterType]
	factoryMutex.RUnlock()

	if !exists {
		return nil, fmt.Errorf("unknown adapter type: %s", config.AdapterType)
	}

	return factory(config)
}

// InitializeStorageAdapters initializes storage adapters during application startup
func InitializeStorageAdapters(configs map[string]StorageAdapterConfig) error {
	for entityType, config := range configs {
		if !config.EnableStorage {
			continue
		}
		
		adapter, err := CreateStorageAdapter(config)
		if err != nil {
			return fmt.Errorf("failed to create storage adapter for %s: %w", entityType, err)
		}
		
		RegisterStorageAdapter(entityType, adapter)
	}
	return nil
}

// CloseAllStorageAdapters closes all global storage adapters
func CloseAllStorageAdapters() error {
	return globalStorageRegistry.CloseAll()
} 