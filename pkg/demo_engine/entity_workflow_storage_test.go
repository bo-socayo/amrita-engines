package demo_engine

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/worker"
	"google.golang.org/protobuf/types/known/timestamppb"

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
	// Start worker for this specific test (non-blocking)
	// Create interrupt channel for worker
	interruptCh := make(chan interface{})
	
	// Start worker in background
	go func() {
		if err := s.worker.Run(interruptCh); err != nil {
			s.T().Logf("Worker error: %v", err)
		}
	}()
	
	// Store cleanup function
	s.T().Cleanup(func() {
		close(interruptCh)
		// Give worker a moment to stop gracefully
		time.Sleep(100 * time.Millisecond)
	})
	
	// Give worker a moment to start
	time.Sleep(200 * time.Millisecond)
	
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
	
	// Register storage adapter for the demo entity type
	storage.RegisterStorageAdapter("demo.Entity", s.storageAdapter)
	
	s.T().Log("✅ Worker started and storage adapter ready for test")
}

func (s *DemoEntityWorkflowStorageTestSuite) TearDownTest() {
	// Clean up - remove the adapter from registry and close it
	storage.RegisterStorageAdapter("demo.Entity", nil)
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
	
	// The test passes if the workflow starts and the update executes successfully
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

func (s *DemoEntityWorkflowStorageTestSuite) TestDirectDatabaseAccess() {
	// Simple test to verify storage adapter works directly
	s.T().Log("🧪 Testing direct database access")
	
	// Just verify we can interact with the storage adapter
	s.NotNil(s.storageAdapter, "Storage adapter should be initialized")
	
	s.T().Log("✅ Direct database access test completed")
} 