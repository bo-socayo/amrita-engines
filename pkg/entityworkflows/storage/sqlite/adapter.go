package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "github.com/mattn/go-sqlite3" // SQLite driver

	"github.com/bo-socayo/amrita-engines/pkg/entityworkflows/storage"
)

// init registers the SQLite adapter factory
func init() {
	storage.RegisterAdapterFactory("sqlite", func(config storage.StorageAdapterConfig) (storage.StorageAdapter, error) {
		return NewSQLiteStorageAdapter(config)
	})
}

// SQLiteStorageAdapter implements StorageAdapter for SQLite
type SQLiteStorageAdapter struct {
	db          *sql.DB
	config      storage.StorageAdapterConfig
	tableName   string
	insertStmt  *sql.Stmt
	healthStmt  *sql.Stmt
}

// NewSQLiteStorageAdapter creates a new SQLite storage adapter
func NewSQLiteStorageAdapter(config storage.StorageAdapterConfig) (*SQLiteStorageAdapter, error) {
	// Open database connection
	db, err := sql.Open("sqlite3", config.ConnectionString)
	if err != nil {
		return nil, fmt.Errorf("failed to open SQLite database: %w", err)
	}

	// Configure connection pool
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)

	// Test connection
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to ping SQLite database: %w", err)
	}

	// Run migrations
	if err := RunMigrations(db, config.TablePrefix); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to run migrations: %w", err)
	}

	tableName := fmt.Sprintf("%s_records", config.TablePrefix)
	adapter := &SQLiteStorageAdapter{
		db:        db,
		config:    config,
		tableName: tableName,
	}

	// Prepare statements
	if err := adapter.prepareStatements(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to prepare statements: %w", err)
	}

	return adapter, nil
}

// prepareStatements prepares commonly used SQL statements
func (s *SQLiteStorageAdapter) prepareStatements() error {
	// Insert statement
	insertSQL := fmt.Sprintf(`
		INSERT INTO %s (
			entity_id, entity_type, sequence_number, processed_at, event_time,
			org_id, team_id, user_id, tenant,
			current_state, previous_state, state_type_name,
			event_envelope, transition_info, event_type_name,
			idempotency_key, workflow_id, run_id, business_logic_version, tags
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, s.tableName)

	var err error
	s.insertStmt, err = s.db.Prepare(insertSQL)
	if err != nil {
		return fmt.Errorf("failed to prepare insert statement: %w", err)
	}

	// Health check statement
	s.healthStmt, err = s.db.Prepare("SELECT 1")
	if err != nil {
		return fmt.Errorf("failed to prepare health statement: %w", err)
	}

	return nil
}

// StoreEntityWorkflowRecord stores an entity workflow record
func (s *SQLiteStorageAdapter) StoreEntityWorkflowRecord(ctx context.Context, record *storage.EntityWorkflowRecord) error {
	// Validate record
	if err := record.Validate(); err != nil {
		return fmt.Errorf("invalid record: %w", err)
	}

	// Serialize tags to JSON
	tagsJSON, err := record.TagsAsJSON()
	if err != nil {
		return fmt.Errorf("failed to serialize tags: %w", err)
	}

	// Retry logic
	var lastErr error
	for attempt := 0; attempt <= s.config.MaxRetries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(s.config.RetryDelay):
				// Continue with retry
			}
		}

		// Execute insert using sequence number from entity workflow
		_, err := s.insertStmt.ExecContext(ctx,
			record.EntityID, record.EntityType, record.SequenceNumber,
			record.ProcessedAt, record.EventTime,
			record.OrgID, record.TeamID, record.UserID, record.Tenant,
			record.CurrentState, record.PreviousState, record.StateTypeName,
			record.EventEnvelope, record.TransitionInfo, record.EventTypeName,
			record.IdempotencyKey, record.WorkflowID, record.RunID,
			record.BusinessLogicVersion, tagsJSON,
		)

		if err == nil {
			return nil // Success
		}

		lastErr = err
		
		// Check if error is retryable
		if !isRetryableError(err) {
			break
		}
	}

	return fmt.Errorf("failed to store record after %d attempts: %w", s.config.MaxRetries+1, lastErr)
}

// CleanupOldRecords removes records older than the cutoff time
func (s *SQLiteStorageAdapter) CleanupOldRecords(ctx context.Context, cutoffTime time.Time) error {
	deleteSQL := fmt.Sprintf("DELETE FROM %s WHERE processed_at < ?", s.tableName)
	
	result, err := s.db.ExecContext(ctx, deleteSQL, cutoffTime)
	if err != nil {
		return fmt.Errorf("failed to cleanup old records: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	fmt.Printf("Cleaned up %d old entity workflow records\n", rowsAffected)
	return nil
}

// IsHealthy checks if the adapter is healthy
func (s *SQLiteStorageAdapter) IsHealthy(ctx context.Context) error {
	var result int
	err := s.healthStmt.QueryRowContext(ctx).Scan(&result)
	if err != nil {
		return fmt.Errorf("health check failed: %w", err)
	}
	return nil
}

// Close closes the database connection and prepared statements
func (s *SQLiteStorageAdapter) Close() error {
	var firstErr error

	// Close prepared statements
	if s.insertStmt != nil {
		if err := s.insertStmt.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if s.healthStmt != nil {
		if err := s.healthStmt.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}

	// Close database
	if s.db != nil {
		if err := s.db.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}

	return firstErr
}

// GetDB returns the underlying database connection (for testing)
func (s *SQLiteStorageAdapter) GetDB() *sql.DB {
	return s.db
}

// isRetryableError determines if an error is retryable
func isRetryableError(err error) bool {
	if err == nil {
		return false
	}

	errStr := err.Error()
	// Check for common retryable SQLite errors
	retryableErrors := []string{
		"database is locked",
		"database is busy",
		"disk I/O error",
		"interrupted",
	}

	for _, retryable := range retryableErrors {
		if contains(errStr, retryable) {
			return true
		}
	}

	return false
}

// contains checks if a string contains a substring (case-insensitive)
func contains(s, substr string) bool {
	return len(s) >= len(substr) && 
		   (s == substr || 
		    (len(s) > len(substr) && 
		     stringContains(s, substr)))
}

// stringContains is a simple substring check
func stringContains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
} 