package demo_engine

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/worker"
	"google.golang.org/protobuf/types/known/timestamppb"
	_ "github.com/mattn/go-sqlite3"

	"github.com/bo-socayo/amrita-engines/pkg/entityworkflows"
	"github.com/bo-socayo/amrita-engines/pkg/entityworkflows/activities"
	"github.com/bo-socayo/amrita-engines/pkg/entityworkflows/storage"
	_ "github.com/bo-socayo/amrita-engines/pkg/entityworkflows/storage/sqlite" // Import to register SQLite adapter
	demov1 "github.com/bo-socayo/amrita-engines/gen/engines/demo/v1"
	entityv1 "github.com/bo-socayo/amrita-engines/gen/entity/v1"
)

type DemoEntityWorkflowStorageTestSuite struct {
	suite.Suite
	
	// Use in-memory dev server
	devServer  *testsuite.DevServer
	client     client.Client
	worker     worker.Worker
	
	// Storage-related fields
	storageAdapter storage.StorageAdapter
	dbFilePath     string
	taskQueue      string
}

func (s *DemoEntityWorkflowStorageTestSuite) SetupSuite() {
	// Start in-memory dev server
	var err error
	s.devServer, err = testsuite.StartDevServer(context.Background(), testsuite.DevServerOptions{
		ClientOptions: &client.Options{},
	})
	s.Require().NoError(err)
	s.Require().NotNil(s.devServer)
	
	// Get client from dev server
	s.client = s.devServer.Client()
	s.Require().NotNil(s.client)
	
	// Create unique task queue for this test suite
	s.taskQueue = "demo-entity-storage-suite-" + time.Now().Format("20060102-150405")
	
	// Create worker (shared across all tests in suite)
	s.worker = worker.New(s.client, s.taskQueue, worker.Options{})
	
	// Register the entity workflow - it comes with activities built-in now!
	s.worker.RegisterWorkflow(DemoEntityWorkflow)
	
	// Only register storage activity since ProcessEventActivity is now built into the workflow
	s.worker.RegisterActivity(activities.StoreEntityWorkflowRecordActivity[*demov1.DemoEngineSignal])
	
	// Start worker once for the entire suite
	go func() {
		err := s.worker.Run(make(chan interface{}))
		if err != nil {
			s.T().Logf("Worker error: %v", err)
		}
	}()
	
	// Give worker a moment to start
	time.Sleep(200 * time.Millisecond)
	
	s.T().Log("✅ Started in-memory Temporal dev server and configured worker (workflow has built-in activities)")
}

func (s *DemoEntityWorkflowStorageTestSuite) TearDownSuite() {
	if s.worker != nil {
		s.worker.Stop()
	}
	if s.devServer != nil {
		err := s.devServer.Stop()
		s.Require().NoError(err)
		s.T().Log("✅ Stopped in-memory Temporal dev server")
	}
}

func (s *DemoEntityWorkflowStorageTestSuite) SetupTest() {
	// [NEW] Initialize storage adapter with real SQLite file (per test)
	tmpFile, err := os.CreateTemp("", "test_demo_storage_*.db")
	s.Require().NoError(err)
	tmpFile.Close() // Close the file so SQLite can use it
	s.dbFilePath = tmpFile.Name() // Store for cleanup
	
	config := storage.StorageAdapterConfig{
		EnableStorage:    true,
		AdapterType:      "sqlite",
		ConnectionString: s.dbFilePath,
		TablePrefix:      "test_demo_entity_workflow",
		MaxRetries:       3,
		RetryDelay:       time.Millisecond * 100,
	}
	
	s.storageAdapter, err = storage.CreateStorageAdapter(config)
	s.Require().NoError(err)
	
	// Register storage adapter for the demo entity type using the correct protobuf full name
	storage.RegisterStorageAdapter("engines.demo.v1.DemoEngineState", s.storageAdapter)
	
	s.T().Log("✅ Storage adapter ready for test")
}

func (s *DemoEntityWorkflowStorageTestSuite) TearDownTest() {
	// Clean up - remove the adapter from registry and close it
	storage.RegisterStorageAdapter("engines.demo.v1.DemoEngineState", nil)
	if s.storageAdapter != nil {
		s.storageAdapter.Close()
	}
	
	// Clean up the temporary database file
	if s.dbFilePath != "" {
		os.Remove(s.dbFilePath)
	}
}

func TestDemoEntityWorkflowStorageTestSuite(t *testing.T) {
	suite.Run(t, new(DemoEntityWorkflowStorageTestSuite))
}

func (s *DemoEntityWorkflowStorageTestSuite) TestDirectDatabaseAccess() {
	// Test to verify storage adapter works directly and data is persisted
	s.T().Log("🧪 Testing direct database access and storage functionality")
	
	// Verify storage adapter is initialized
	s.NotNil(s.storageAdapter, "Storage adapter should be initialized")
	
	// Test that we can open a direct database connection to the same file
	db, err := sql.Open("sqlite3", s.dbFilePath)
	s.Require().NoError(err)
	defer db.Close()
	
	// Test database connection
	err = db.Ping()
	s.Require().NoError(err)
	s.T().Log("✅ Successfully connected to SQLite database")
	
	// Check that the table exists
	tableName := "test_demo_entity_workflow_records"
	checkTableSQL := `SELECT name FROM sqlite_master WHERE type='table' AND name=?`
	var foundTable string
	err = db.QueryRow(checkTableSQL, tableName).Scan(&foundTable)
	s.Require().NoError(err)
	s.Equal(tableName, foundTable)
	s.T().Log("✅ Storage table exists in database")
	
	// Check table schema
	schemaSQL := `PRAGMA table_info(test_demo_entity_workflow_records)`
	rows, err := db.Query(schemaSQL)
	s.Require().NoError(err)
	defer rows.Close()
	
	columnCount := 0
	expectedColumns := []string{"entity_id", "entity_type", "sequence_number", "processed_at", 
		"current_state", "event_envelope", "org_id", "user_id", "idempotency_key"}
	foundColumns := make(map[string]bool)
	
	for rows.Next() {
		var cid int
		var name, dataType string
		var notNull, pk int
		var defaultValue *string
		
		err := rows.Scan(&cid, &name, &dataType, &notNull, &defaultValue, &pk)
		s.Require().NoError(err)
		foundColumns[name] = true
		columnCount++
	}
	
	s.True(columnCount > 10, "Table should have many columns")
	for _, expectedCol := range expectedColumns {
		s.True(foundColumns[expectedCol], "Expected column %s should exist", expectedCol)
	}
	s.T().Log("✅ Table schema looks correct")
	
	// Test that table is initially empty
	countSQL := `SELECT COUNT(*) FROM test_demo_entity_workflow_records`
	var initialCount int
	err = db.QueryRow(countSQL).Scan(&initialCount)
	s.Require().NoError(err)
	s.Equal(0, initialCount, "Table should be empty initially")
	
	s.T().Log("✅ Direct database access test completed")
}

func (s *DemoEntityWorkflowStorageTestSuite) TestDemoEntityWorkflow_BasicIncrementWithStorage() {
	// Create initial state
	initialState := &demov1.DemoEngineState{
		CurrentValue: 0,
		Config: &demov1.DemoConfig{
			MaxValue:         100,
			DefaultIncrement: 1,
			AllowNegative:    false,
			Description:      "Test demo engine with storage",
		},
		History:     []*demov1.CounterEvent{},
		LastUpdated: nil,
		NextEventId: 1,
	}

	// Create workflow params
	params := entityworkflows.EntityWorkflowParams[*demov1.DemoEngineState]{
		EntityId:     "test-demo-storage-1",
		InitialState: initialState,
	}

	// Start the workflow
	s.T().Log("🚀 Starting long-running entity workflow")
	workflowRun, err := s.client.ExecuteWorkflow(context.Background(), client.StartWorkflowOptions{
		ID:        "test-storage-workflow-" + time.Now().Format("20060102-150405-000"),
		TaskQueue: s.taskQueue,
	}, DemoEntityWorkflow, params)
	s.Require().NoError(err)
	s.T().Log("📋 Workflow started and ready for updates")

	// Give workflow a moment to initialize
	time.Sleep(300 * time.Millisecond)

	// Create increment signal 
	incrementSignal := &demov1.DemoEngineSignal{
		Signal: &demov1.DemoEngineSignal_Increment{
			Increment: &demov1.IncrementSignal{
				Amount:    5,
				Reason:    "Test increment via update with storage",
				Timestamp: timestamppb.Now(),
			},
		},
	}

	// Create request context
	requestCtx := &entityv1.RequestContext{
		UserId:         "test-user-1",
		OrgId:          "test-org-1",
		TeamId:         "test-team-1",
		Environment:    "test",
		Tenant:         "test-tenant",
		IdempotencyKey: "update-increment-storage-1",
		RequestTime:    timestamppb.Now(),
	}

	// Send update to the running workflow
	s.T().Log("⏰ Sending update via processEvent")
	updateHandle, err := s.client.UpdateWorkflow(context.Background(), client.UpdateWorkflowOptions{
		WorkflowID:   workflowRun.GetID(),
		RunID:        workflowRun.GetRunID(),
		UpdateName:   "processEvent",
		WaitForStage: client.WorkflowUpdateStageCompleted,
		Args:         []interface{}{requestCtx, incrementSignal},
	})
	s.Require().NoError(err)
	
	// Get the update result with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	
	var updateResult *demov1.DemoEngineState
	err = updateHandle.Get(ctx, &updateResult)
	s.Require().NoError(err)
	s.T().Log("🎯 Update completed successfully!")
	
	// Verify the update worked
	s.NotNil(updateResult)
	s.Equal(int64(5), updateResult.CurrentValue, "Value should be incremented by 5")
	s.Len(updateResult.History, 1, "Should have one event in history")
	
	s.T().Logf("📊 Updated state - CurrentValue: %d, History length: %d", 
		updateResult.CurrentValue, len(updateResult.History))
	
	// 🔍 Now verify the data was actually stored in the database
	s.T().Log("🔍 Verifying data was stored in SQLite database")
	
	// Wait a moment for storage to complete (storage happens asynchronously)
	time.Sleep(500 * time.Millisecond)
	
	// Open database connection to verify storage
	db, err := sql.Open("sqlite3", s.dbFilePath)
	s.Require().NoError(err)
	defer db.Close()
	
	// Query the records table
	querySQL := `SELECT entity_id, entity_type, sequence_number, org_id, user_id, team_id, 
		tenant, idempotency_key, workflow_id, run_id, state_type_name, event_type_name 
		FROM test_demo_entity_workflow_records 
		WHERE entity_id = ? ORDER BY sequence_number`
	
	rows, err := db.Query(querySQL, "test-demo-storage-1")
	s.Require().NoError(err)
	defer rows.Close()
	
	recordCount := 0
	for rows.Next() {
		var entityID, entityType, orgID, userID, teamID, tenant, idempotencyKey string
		var workflowID, runID, stateTypeName, eventTypeName string
		var sequenceNumber int64
		
		err := rows.Scan(&entityID, &entityType, &sequenceNumber, &orgID, &userID, &teamID,
			&tenant, &idempotencyKey, &workflowID, &runID, &stateTypeName, &eventTypeName)
		s.Require().NoError(err)
		
		// Verify the stored record matches our expectations
		s.Equal("test-demo-storage-1", entityID)
		s.Equal("engines.demo.v1.DemoEngineState", entityType)
		s.Equal(int64(1), sequenceNumber) // First event should be sequence 1
		s.Equal("test-org-1", orgID)
		s.Equal("test-user-1", userID)
		s.Equal("test-team-1", teamID)
		s.Equal("test-tenant", tenant)
		s.Equal("update-increment-storage-1", idempotencyKey)
		s.Equal(workflowRun.GetID(), workflowID)
		s.Equal(workflowRun.GetRunID(), runID)
		s.Equal("DemoEngineState", stateTypeName)
		s.Equal("DemoEngineSignal", eventTypeName)
		
		recordCount++
		s.T().Logf("📋 Found database record: entity_id=%s, sequence=%d, org_id=%s", 
			entityID, sequenceNumber, orgID)
	}
	
	s.Equal(1, recordCount, "Should have exactly one record stored")
	
	// Verify the total count
	var totalRecords int
	err = db.QueryRow("SELECT COUNT(*) FROM test_demo_entity_workflow_records").Scan(&totalRecords)
	s.Require().NoError(err)
	s.Equal(1, totalRecords, "Should have exactly one total record in database")
	
	s.T().Log("✅ Verified data was correctly stored in SQLite database!")
	s.T().Log("✅ Test completed - entity workflow is running and configured for storage")
}

func (s *DemoEntityWorkflowStorageTestSuite) TestSimpleUpdateWithoutStorage() {
	// This test matches the working update test pattern exactly to verify updates work
	
	// Create initial state
	initialState := &demov1.DemoEngineState{
		CurrentValue: 0,
		Config: &demov1.DemoConfig{
			MaxValue:         100,
			DefaultIncrement: 1,
			AllowNegative:    false,
			Description:      "Test demo engine",
		},
		History:     []*demov1.CounterEvent{},
		LastUpdated: nil,
		NextEventId: 1,
	}

	// Create workflow params
	params := entityworkflows.EntityWorkflowParams[*demov1.DemoEngineState]{
		EntityId:     "test-simple-update",
		InitialState: initialState,
	}

	// Start the workflow
	s.T().Log("🚀 Starting long-running entity workflow")
	workflowRun, err := s.client.ExecuteWorkflow(context.Background(), client.StartWorkflowOptions{
		ID:        "test-simple-update-workflow-" + time.Now().Format("20060102-150405-000"),
		TaskQueue: s.taskQueue,
	}, DemoEntityWorkflow, params)
	s.Require().NoError(err)
	s.T().Log("📋 Workflow started and ready for updates")

	// Give workflow a moment to initialize
	time.Sleep(300 * time.Millisecond)

	// Create increment signal 
	incrementSignal := &demov1.DemoEngineSignal{
		Signal: &demov1.DemoEngineSignal_Increment{
			Increment: &demov1.IncrementSignal{
				Amount:    10,
				Reason:    "Simple update test",
				Timestamp: timestamppb.Now(),
			},
		},
	}

	// Create request context
	requestCtx := &entityv1.RequestContext{
		UserId:         "test-user-simple",
		OrgId:          "test-org-simple",
		TeamId:         "test-team-simple",
		Environment:    "test",
		Tenant:         "test-tenant",
		IdempotencyKey: "simple-update-test",
		RequestTime:    timestamppb.Now(),
	}

	// Send update to the running workflow
	s.T().Log("⏰ Sending update via processEvent")
	updateHandle, err := s.client.UpdateWorkflow(context.Background(), client.UpdateWorkflowOptions{
		WorkflowID:   workflowRun.GetID(),
		RunID:        workflowRun.GetRunID(),
		UpdateName:   "processEvent",
		WaitForStage: client.WorkflowUpdateStageCompleted,
		Args:         []interface{}{requestCtx, incrementSignal},
	})
	s.Require().NoError(err)
	
	// Get the update result with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	
	var updateResult *demov1.DemoEngineState
	err = updateHandle.Get(ctx, &updateResult)
	s.Require().NoError(err)
	s.T().Log("🎯 Update completed successfully!")
	
	// Verify the update worked
	s.NotNil(updateResult)
	s.Equal(int64(10), updateResult.CurrentValue, "Value should be incremented by 10")
	s.Len(updateResult.History, 1, "Should have one event in history")
	
	s.T().Logf("📊 Updated state - CurrentValue: %d, History length: %d", 
		updateResult.CurrentValue, len(updateResult.History))
	
	s.T().Log("✅ Simple update test completed successfully!")
}

func (s *DemoEntityWorkflowStorageTestSuite) TestMultipleUpdatesWithDatabaseVerification() {
	// Test multiple updates and verify all are stored correctly in the database
	s.T().Log("🧪 Testing multiple updates with database verification")
	
	initialState := &demov1.DemoEngineState{
		CurrentValue: 0,
		Config: &demov1.DemoConfig{
			MaxValue:         100,
			DefaultIncrement: 1,
			AllowNegative:    false,
			Description:      "Test demo engine for multiple updates",
		},
		History:     []*demov1.CounterEvent{},
		LastUpdated: nil,
		NextEventId: 1,
	}

	params := entityworkflows.EntityWorkflowParams[*demov1.DemoEngineState]{
		EntityId:     "test-multi-storage",
		InitialState: initialState,
	}

	// Start the workflow
	workflowRun, err := s.client.ExecuteWorkflow(context.Background(), client.StartWorkflowOptions{
		ID:        "test-multi-storage-workflow-" + time.Now().Format("20060102-150405-000"),
		TaskQueue: s.taskQueue,
	}, DemoEntityWorkflow, params)
	s.Require().NoError(err)
	time.Sleep(300 * time.Millisecond)

	// Send first update
	s.T().Log("⏰ Sending first increment (3)")
	incrementSignal1 := &demov1.DemoEngineSignal{
		Signal: &demov1.DemoEngineSignal_Increment{
			Increment: &demov1.IncrementSignal{Amount: 3, Reason: "First increment", Timestamp: timestamppb.Now()},
		},
	}
	
	requestCtx1 := &entityv1.RequestContext{
		UserId: "test-user-multi", OrgId: "test-org-multi", TeamId: "test-team-multi",
		Environment: "test", Tenant: "test-tenant", IdempotencyKey: "multi-1", RequestTime: timestamppb.Now(),
	}
	
	updateHandle1, err := s.client.UpdateWorkflow(context.Background(), client.UpdateWorkflowOptions{
		WorkflowID: workflowRun.GetID(), RunID: workflowRun.GetRunID(), UpdateName: "processEvent",
		WaitForStage: client.WorkflowUpdateStageCompleted, Args: []interface{}{requestCtx1, incrementSignal1},
	})
	s.Require().NoError(err)
	
	ctx1, cancel1 := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel1()
	var result1 *demov1.DemoEngineState
	err = updateHandle1.Get(ctx1, &result1)
	s.Require().NoError(err)
	s.Equal(int64(3), result1.CurrentValue)

	// Send second update
	s.T().Log("⏰ Sending second increment (7)")
	incrementSignal2 := &demov1.DemoEngineSignal{
		Signal: &demov1.DemoEngineSignal_Increment{
			Increment: &demov1.IncrementSignal{Amount: 7, Reason: "Second increment", Timestamp: timestamppb.Now()},
		},
	}
	
	requestCtx2 := &entityv1.RequestContext{
		UserId: "test-user-multi", OrgId: "test-org-multi", TeamId: "test-team-multi",
		Environment: "test", Tenant: "test-tenant", IdempotencyKey: "multi-2", RequestTime: timestamppb.Now(),
	}
	
	updateHandle2, err := s.client.UpdateWorkflow(context.Background(), client.UpdateWorkflowOptions{
		WorkflowID: workflowRun.GetID(), RunID: workflowRun.GetRunID(), UpdateName: "processEvent",
		WaitForStage: client.WorkflowUpdateStageCompleted, Args: []interface{}{requestCtx2, incrementSignal2},
	})
	s.Require().NoError(err)
	
	ctx2, cancel2 := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel2()
	var result2 *demov1.DemoEngineState
	err = updateHandle2.Get(ctx2, &result2)
	s.Require().NoError(err)
	s.Equal(int64(10), result2.CurrentValue)

	// Wait for storage to complete
	time.Sleep(1 * time.Second)
	
	// 🔍 Verify both records are in the database
	s.T().Log("🔍 Verifying both updates were stored in database")
	db, err := sql.Open("sqlite3", s.dbFilePath)
	s.Require().NoError(err)
	defer db.Close()
	
	querySQL := `SELECT entity_id, sequence_number, idempotency_key, processed_at 
		FROM test_demo_entity_workflow_records 
		WHERE entity_id = ? ORDER BY sequence_number`
	
	rows, err := db.Query(querySQL, "test-multi-storage")
	s.Require().NoError(err)
	defer rows.Close()
	
	records := []struct{
		entityID string
		sequenceNumber int64
		idempotencyKey string
		processedAt time.Time
	}{}
	
	for rows.Next() {
		var record struct{
			entityID string
			sequenceNumber int64
			idempotencyKey string
			processedAt time.Time
		}
		err := rows.Scan(&record.entityID, &record.sequenceNumber, &record.idempotencyKey, &record.processedAt)
		s.Require().NoError(err)
		records = append(records, record)
	}
	
	s.Len(records, 2, "Should have exactly 2 records stored")
	
	// Verify first record
	s.Equal("test-multi-storage", records[0].entityID)
	s.Equal(int64(1), records[0].sequenceNumber)
	s.Equal("multi-1", records[0].idempotencyKey)
	
	// Verify second record  
	s.Equal("test-multi-storage", records[1].entityID)
	s.Equal(int64(2), records[1].sequenceNumber)
	s.Equal("multi-2", records[1].idempotencyKey)
	
	// Verify timestamps are ordered correctly
	s.True(records[1].processedAt.After(records[0].processedAt) || records[1].processedAt.Equal(records[0].processedAt),
		"Second record should be processed after or at same time as first record")
	
	s.T().Log("✅ Multiple updates with database verification completed successfully!")
} 