package sqlite

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bo-socayo/amrita-engines/pkg/entityworkflows/storage"
)

func TestSQLiteStorageAdapter(t *testing.T) {
	// Create in-memory SQLite for testing
	config := storage.StorageAdapterConfig{
		EnableStorage:    true,
		AdapterType:      "sqlite",
		ConnectionString: ":memory:",
		TablePrefix:      "test_entity_workflow",
		MaxRetries:       3,
		RetryDelay:       100 * time.Millisecond,
	}

	adapter, err := NewSQLiteStorageAdapter(config)
	require.NoError(t, err)
	defer adapter.Close()

	t.Run("health check", func(t *testing.T) {
		err := adapter.IsHealthy(context.Background())
		assert.NoError(t, err)
	})

	t.Run("store and verify record", func(t *testing.T) {
		// Create test record
		now := time.Now()
		record := &storage.EntityWorkflowRecord{
			EntityID:             "test-entity-1",
			EntityType:           "demo.Entity",
			SequenceNumber:       1,
			ProcessedAt:          now,
			EventTime:            now.Add(-1 * time.Minute),
			OrgID:                "test-org",
			TeamID:               "test-team",
			UserID:               "test-user",
			Tenant:               "test-tenant",
			CurrentState:         []byte("serialized-current-state"),
			PreviousState:        []byte("serialized-previous-state"),
			StateTypeName:        "DemoEngineState",
			EventEnvelope:        []byte("serialized-event"),
			TransitionInfo:       []byte("serialized-transition"),
			EventTypeName:        "DemoEngineSignal",
			IdempotencyKey:       "test-idempotency-key",
			WorkflowID:           "test-workflow-id",
			RunID:                "test-run-id",
			BusinessLogicVersion: "1.0.0",
		}
		record.SetTag("environment", "test")
		record.SetTag("feature", "storage")

		// Store record
		err := adapter.StoreEntityWorkflowRecord(context.Background(), record)
		require.NoError(t, err)

		// Verify record was stored by checking database directly
		db := adapter.GetDB()
		var count int
		err = db.QueryRow("SELECT COUNT(*) FROM test_entity_workflow_records WHERE entity_id = ? AND entity_type = ?",
			"test-entity-1", "demo.Entity").Scan(&count)
		require.NoError(t, err)
		assert.Equal(t, 1, count)

		// Verify record details
		var entityID, entityType string
		var sequenceNumber int64
		var tags string
		err = db.QueryRow(`SELECT entity_id, entity_type, sequence_number, tags 
						   FROM test_entity_workflow_records 
						   WHERE entity_id = ? AND entity_type = ?`,
			"test-entity-1", "demo.Entity").Scan(&entityID, &entityType, &sequenceNumber, &tags)
		require.NoError(t, err)
		assert.Equal(t, "test-entity-1", entityID)
		assert.Equal(t, "demo.Entity", entityType)
		assert.Equal(t, int64(1), sequenceNumber)
		assert.Contains(t, tags, "environment")
		assert.Contains(t, tags, "test")
	})

	t.Run("store multiple records", func(t *testing.T) {
		now := time.Now()

		// Store multiple records for the same entity
		for i := 2; i <= 5; i++ {
			record := &storage.EntityWorkflowRecord{
				EntityID:       "test-entity-1",
				EntityType:     "demo.Entity",
				SequenceNumber: int64(i),
				ProcessedAt:    now.Add(time.Duration(i) * time.Second),
				EventTime:      now.Add(time.Duration(i-1) * time.Second),
				OrgID:          "test-org",
				CurrentState:   []byte("serialized-state"),
				EventEnvelope:  []byte("serialized-event"),
				TransitionInfo: []byte("serialized-transition"),
				StateTypeName:  "DemoEngineState",
				EventTypeName:  "DemoEngineSignal",
			}

			err := adapter.StoreEntityWorkflowRecord(context.Background(), record)
			require.NoError(t, err)
		}

		// Verify all records were stored
		db := adapter.GetDB()
		var count int
		err = db.QueryRow("SELECT COUNT(*) FROM test_entity_workflow_records WHERE entity_id = ?",
			"test-entity-1").Scan(&count)
		require.NoError(t, err)
		assert.Equal(t, 5, count) // Including the first record from previous test
	})

	t.Run("validation errors", func(t *testing.T) {
		// Test missing entity ID
		record := &storage.EntityWorkflowRecord{
			EntityType:     "demo.Entity",
			SequenceNumber: 1,
			ProcessedAt:    time.Now(),
			CurrentState:   []byte("state"),
			EventEnvelope:  []byte("event"),
		}
		err := adapter.StoreEntityWorkflowRecord(context.Background(), record)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "entity_id is required")

		// Test missing current state
		record = &storage.EntityWorkflowRecord{
			EntityID:       "test-entity",
			EntityType:     "demo.Entity",
			SequenceNumber: 1,
			ProcessedAt:    time.Now(),
			EventEnvelope:  []byte("event"),
		}
		err = adapter.StoreEntityWorkflowRecord(context.Background(), record)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "current_state is required")
	})

	t.Run("cleanup old records", func(t *testing.T) {
		// Add old record
		oldRecord := &storage.EntityWorkflowRecord{
			EntityID:       "old-entity",
			EntityType:     "demo.Entity",
			SequenceNumber: 1,
			ProcessedAt:    time.Now().Add(-24 * time.Hour), // 1 day ago
			EventTime:      time.Now().Add(-24 * time.Hour),
			OrgID:          "test-org",
			CurrentState:   []byte("old-state"),
			EventEnvelope:  []byte("old-event"),
			TransitionInfo: []byte("old-transition"),
			StateTypeName:  "DemoEngineState",
			EventTypeName:  "DemoEngineSignal",
		}

		err := adapter.StoreEntityWorkflowRecord(context.Background(), oldRecord)
		require.NoError(t, err)

		// Cleanup records older than 12 hours
		cutoffTime := time.Now().Add(-12 * time.Hour)
		err = adapter.CleanupOldRecords(context.Background(), cutoffTime)
		require.NoError(t, err)

		// Verify old record was deleted
		db := adapter.GetDB()
		var count int
		err = db.QueryRow("SELECT COUNT(*) FROM test_entity_workflow_records WHERE entity_id = ?",
			"old-entity").Scan(&count)
		require.NoError(t, err)
		assert.Equal(t, 0, count)
	})
}

func TestSQLiteStorageAdapter_ConcurrentWrites(t *testing.T) {
	// Create temporary database file for concurrent testing
	tmpFile, err := os.CreateTemp("", "test_concurrent_*.db")
	require.NoError(t, err)
	tmpFile.Close()
	defer os.Remove(tmpFile.Name())

	config := storage.StorageAdapterConfig{
		EnableStorage:    true,
		AdapterType:      "sqlite",
		ConnectionString: tmpFile.Name(),
		TablePrefix:      "test_concurrent",
		MaxRetries:       3,
		RetryDelay:       50 * time.Millisecond,
	}

	adapter, err := NewSQLiteStorageAdapter(config)
	require.NoError(t, err)
	defer adapter.Close()

	// Test concurrent writes
	const numWorkers = 10
	const recordsPerWorker = 5

	done := make(chan error, numWorkers)

	for workerID := 0; workerID < numWorkers; workerID++ {
		go func(id int) {
			for i := 0; i < recordsPerWorker; i++ {
				record := &storage.EntityWorkflowRecord{
					EntityID:       fmt.Sprintf("entity-%d", id),
					EntityType:     "demo.Entity",
					SequenceNumber: int64(i + 1),
					ProcessedAt:    time.Now(),
					EventTime:      time.Now(),
					OrgID:          "test-org",
					CurrentState:   []byte("state"),
					EventEnvelope:  []byte("event"),
					TransitionInfo: []byte("transition"),
					StateTypeName:  "DemoEngineState",
					EventTypeName:  "DemoEngineSignal",
				}

				if err := adapter.StoreEntityWorkflowRecord(context.Background(), record); err != nil {
					done <- err
					return
				}
			}
			done <- nil
		}(workerID)
	}

	// Wait for all workers to complete
	for i := 0; i < numWorkers; i++ {
		select {
		case err := <-done:
			require.NoError(t, err)
		case <-time.After(10 * time.Second):
			t.Fatal("Timeout waiting for concurrent writes")
		}
	}

	// Verify all records were stored
	db := adapter.GetDB()
	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM test_concurrent_records").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, numWorkers*recordsPerWorker, count)
} 