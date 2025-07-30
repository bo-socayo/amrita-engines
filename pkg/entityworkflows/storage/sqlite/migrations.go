package sqlite

import (
	"database/sql"
	"fmt"
)

// createEntityWorkflowTableSQL defines the SQL schema for entity workflow records
const createEntityWorkflowTableSQL = `
CREATE TABLE IF NOT EXISTS %s_records (
    entity_id TEXT NOT NULL,
    entity_type TEXT NOT NULL,
    sequence_number INTEGER NOT NULL,
    processed_at TIMESTAMP NOT NULL,
    event_time TIMESTAMP NOT NULL,
    org_id TEXT NOT NULL,
    team_id TEXT,
    user_id TEXT,
    tenant TEXT,
    current_state BLOB NOT NULL,
    previous_state BLOB,
    state_type_name TEXT NOT NULL,
    event_envelope BLOB NOT NULL,
    transition_info BLOB NOT NULL,
    event_type_name TEXT NOT NULL,
    idempotency_key TEXT,
    workflow_id TEXT,
    run_id TEXT,
    business_logic_version TEXT,
    tags TEXT DEFAULT '{}',
    
    PRIMARY KEY (entity_id, entity_type, sequence_number)
);`

// createIndexesSQL defines the indexes for performance
const createIndexesSQL = `
CREATE INDEX IF NOT EXISTS idx_%s_entity_lookup ON %s_records (entity_id, entity_type);
CREATE INDEX IF NOT EXISTS idx_%s_org_lookup ON %s_records (org_id, entity_type, processed_at);
CREATE INDEX IF NOT EXISTS idx_%s_processed_at ON %s_records (processed_at);
CREATE INDEX IF NOT EXISTS idx_%s_idempotency ON %s_records (idempotency_key) WHERE idempotency_key IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_%s_workflow ON %s_records (workflow_id, run_id);`

// Migration represents a database migration
type Migration struct {
	Version     int
	Description string
	SQL         string
}

// getMigrations returns all database migrations
func getMigrations(tablePrefix string) []Migration {
	return []Migration{
		{
			Version:     1,
			Description: "Create entity workflow records table",
			SQL:         fmt.Sprintf(createEntityWorkflowTableSQL, tablePrefix),
		},
		{
			Version:     2,
			Description: "Create indexes for entity workflow records",
			SQL: fmt.Sprintf(createIndexesSQL,
				tablePrefix, tablePrefix, // entity lookup
				tablePrefix, tablePrefix, // org lookup
				tablePrefix, tablePrefix, // processed_at
				tablePrefix, tablePrefix, // idempotency
				tablePrefix, tablePrefix, // workflow
			),
		},
	}
}

// createMigrationsTable creates the migrations tracking table
func createMigrationsTable(db *sql.DB) error {
	createSQL := `
	CREATE TABLE IF NOT EXISTS schema_migrations (
		version INTEGER PRIMARY KEY,
		description TEXT NOT NULL,
		applied_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);`
	
	_, err := db.Exec(createSQL)
	return err
}

// getAppliedMigrations returns the list of applied migration versions
func getAppliedMigrations(db *sql.DB) (map[int]bool, error) {
	applied := make(map[int]bool)
	
	rows, err := db.Query("SELECT version FROM schema_migrations")
	if err != nil {
		return applied, err
	}
	defer rows.Close()
	
	for rows.Next() {
		var version int
		if err := rows.Scan(&version); err != nil {
			return applied, err
		}
		applied[version] = true
	}
	
	return applied, rows.Err()
}

// recordMigration records that a migration has been applied
func recordMigration(db *sql.DB, migration Migration) error {
	_, err := db.Exec(
		"INSERT INTO schema_migrations (version, description) VALUES (?, ?)",
		migration.Version, migration.Description,
	)
	return err
}

// RunMigrations applies all pending migrations
func RunMigrations(db *sql.DB, tablePrefix string) error {
	// Create migrations table
	if err := createMigrationsTable(db); err != nil {
		return fmt.Errorf("failed to create migrations table: %w", err)
	}
	
	// Get applied migrations
	applied, err := getAppliedMigrations(db)
	if err != nil {
		return fmt.Errorf("failed to get applied migrations: %w", err)
	}
	
	// Apply pending migrations
	migrations := getMigrations(tablePrefix)
	for _, migration := range migrations {
		if applied[migration.Version] {
			continue // Already applied
		}
		
		// Apply migration
		if _, err := db.Exec(migration.SQL); err != nil {
			return fmt.Errorf("failed to apply migration %d (%s): %w", 
				migration.Version, migration.Description, err)
		}
		
		// Record migration
		if err := recordMigration(db, migration); err != nil {
			return fmt.Errorf("failed to record migration %d: %w", migration.Version, err)
		}
		
		fmt.Printf("Applied migration %d: %s\n", migration.Version, migration.Description)
	}
	
	return nil
} 