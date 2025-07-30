package storage

import (
	"context"
	"time"
)

// StorageAdapter defines the interface for persisting entity workflow data
type StorageAdapter interface {
	// Store entity state and related data after successful event processing
	StoreEntityWorkflowRecord(ctx context.Context, record *EntityWorkflowRecord) error
	
	// Cleanup methods (optional - can be handled by separate maintenance processes)
	CleanupOldRecords(ctx context.Context, cutoffTime time.Time) error
	
	// Health check
	IsHealthy(ctx context.Context) error
	
	// Close releases any resources held by the adapter
	Close() error
}

// StorageAdapterConfig provides configuration for storage adapters
type StorageAdapterConfig struct {
	EnableStorage    bool          `json:"enable_storage" yaml:"enable_storage"`
	AdapterType      string        `json:"adapter_type" yaml:"adapter_type"`         // sqlite, postgres, etc.
	ConnectionString string        `json:"connection_string" yaml:"connection_string"`
	TablePrefix      string        `json:"table_prefix" yaml:"table_prefix"`
	MaxRetries       int           `json:"max_retries" yaml:"max_retries"`
	RetryDelay       time.Duration `json:"retry_delay" yaml:"retry_delay"`
}

// DefaultStorageAdapterConfig returns a config with sensible defaults
func DefaultStorageAdapterConfig() StorageAdapterConfig {
	return StorageAdapterConfig{
		EnableStorage:    false,
		AdapterType:      "sqlite",
		TablePrefix:      "entity_workflow",
		MaxRetries:       3,
		RetryDelay:       100 * time.Millisecond,
	}
} 