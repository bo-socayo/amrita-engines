package demo_engine

import (
	"context"
	"fmt"
	"testing"
	"time"

	demov1 "github.com/bo-socayo/amrita-engines/gen/engines/demo/v1"
	entityv1 "github.com/bo-socayo/amrita-engines/gen/entity/v1"
	"github.com/bo-socayo/amrita-engines/pkg/entityworkflows"
	"github.com/stretchr/testify/suite"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/worker"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// DemoEntityQueryIntegrationTestSuite tests query handlers in the entity workflow
type DemoEntityQueryIntegrationTestSuite struct {
	suite.Suite
	
	// Use in-memory dev server
	devServer  *testsuite.DevServer
	client     client.Client
	worker     worker.Worker
	taskQueue  string
}

func (s *DemoEntityQueryIntegrationTestSuite) SetupSuite() {
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
	s.taskQueue = "demo-entity-query-suite-" + time.Now().Format("20060102-150405")
	
	// Create worker (shared across all tests in suite)
	s.worker = worker.New(s.client, s.taskQueue, worker.Options{})
	
	// Register the entity workflow - it comes with activities built-in now!
	s.worker.RegisterWorkflow(DemoEntityWorkflow)
	
	// Start worker once for the entire suite
	go func() {
		err := s.worker.Run(make(chan interface{}))
		if err != nil {
			s.T().Logf("Worker error: %v", err)
		}
	}()
	
	// Give worker a moment to start
	time.Sleep(200 * time.Millisecond)
	
	s.T().Log("✅ Started in-memory Temporal dev server for query tests")
}

func (s *DemoEntityQueryIntegrationTestSuite) TearDownSuite() {
	if s.worker != nil {
		s.worker.Stop()
	}
	if s.devServer != nil {
		err := s.devServer.Stop()
		s.Require().NoError(err)
		s.T().Log("✅ Stopped in-memory Temporal dev server")
	}
}

func (s *DemoEntityQueryIntegrationTestSuite) SetupTest() {
	// No per-test setup needed
}

func (s *DemoEntityQueryIntegrationTestSuite) TearDownTest() {
	// No per-test cleanup needed
}

func TestDemoEntityQueryIntegrationTestSuite(t *testing.T) {
	suite.Run(t, new(DemoEntityQueryIntegrationTestSuite))
}

func (s *DemoEntityQueryIntegrationTestSuite) TestQueryBeforeInitialization() {
	// Test querying entity state before any operations
	params := entityworkflows.EntityWorkflowParams[*demov1.DemoEngineState]{
		EntityId:     "test-query-uninit",
		InitialState: ApplyBusinessDefaults(nil),
	}

	// Start the workflow
	s.T().Log("🚀 Starting workflow for query before init test")
	workflowRun, err := s.client.ExecuteWorkflow(context.Background(), client.StartWorkflowOptions{
		ID:        "test-query-uninit-workflow-" + time.Now().Format("20060102-150405-000"),
		TaskQueue: s.taskQueue,
	}, DemoEntityWorkflow, params)
	s.Require().NoError(err)
	s.T().Log("📋 Workflow started")

	// Give workflow a moment to initialize
	time.Sleep(300 * time.Millisecond)

	// Query before any updates
	s.T().Log("🔍 Querying entity state before any updates")
	response, err := s.client.QueryWorkflow(context.Background(), workflowRun.GetID(), workflowRun.GetRunID(), "getEntityState")
	s.NoError(err)

	var queryResponse entityworkflows.EntityQueryResponse[*demov1.DemoEngineState]
	err = response.Get(&queryResponse)
	s.NoError(err)

	s.True(queryResponse.IsInitialized)
	s.NotEmpty(queryResponse.CurrentStateJSON)
	// Metadata may be nil before any operations
	
	// Verify initial state values (currentValue may be omitted when 0 in protobuf JSON)
	s.Contains(queryResponse.CurrentStateJSON, `"nextEventId":"1"`)
	s.Contains(queryResponse.CurrentStateJSON, `"config"`)
	s.Contains(queryResponse.CurrentStateJSON, `"defaultIncrement":"1"`)
	
	s.T().Log("✅ Query before initialization test completed successfully!")
}

func (s *DemoEntityQueryIntegrationTestSuite) TestQueryAfterUpdates() {
	params := entityworkflows.EntityWorkflowParams[*demov1.DemoEngineState]{
		EntityId:     "test-query-after-updates",
		InitialState: ApplyBusinessDefaults(nil),
	}

	// Start the workflow
	s.T().Log("🚀 Starting workflow for query after updates test")
	workflowRun, err := s.client.ExecuteWorkflow(context.Background(), client.StartWorkflowOptions{
		ID:        "test-query-after-updates-workflow-" + time.Now().Format("20060102-150405-000"),
		TaskQueue: s.taskQueue,
	}, DemoEntityWorkflow, params)
	s.Require().NoError(err)
	s.T().Log("📋 Workflow started")

	// Give workflow a moment to initialize
	time.Sleep(300 * time.Millisecond)

	// Send update first
	requestCtx := &entityv1.RequestContext{
		UserId:         "test-user",
		OrgId:          "test-org",
		TeamId:         "test-team",
		Environment:    "test",
		Tenant:         "test-tenant",
		IdempotencyKey: "test-query-1",
		RequestTime:    timestamppb.Now(),
	}

	incrementSignal := &demov1.DemoEngineSignal{
		Signal: &demov1.DemoEngineSignal_Increment{
			Increment: &demov1.IncrementSignal{
				Amount:    5,
				Reason:    "Test increment for query",
				Timestamp: timestamppb.Now(),
			},
		},
	}

	// Send update
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

	// Query state after update completes
	s.T().Log("🔍 Querying entity state after update")
	response, err := s.client.QueryWorkflow(context.Background(), workflowRun.GetID(), workflowRun.GetRunID(), "getEntityState")
	s.NoError(err)

	var queryResponse entityworkflows.EntityQueryResponse[*demov1.DemoEngineState]
	err = response.Get(&queryResponse)
	s.NoError(err)

	s.True(queryResponse.IsInitialized)
	s.NotEmpty(queryResponse.CurrentStateJSON)
	
	// Verify updated state values
	s.Contains(queryResponse.CurrentStateJSON, `"currentValue":"5"`)
	s.Contains(queryResponse.CurrentStateJSON, `"nextEventId":"2"`)
	s.Contains(queryResponse.CurrentStateJSON, `"history"`)
	
	// Verify metadata contains request info
	s.Equal("test-user", queryResponse.Metadata.UserId)
	s.Equal("test-org", queryResponse.Metadata.OrgId)
	
	s.T().Log("✅ Query after updates test completed successfully!")
}

func (s *DemoEntityQueryIntegrationTestSuite) TestQueryAfterMultipleOperations() {
	params := entityworkflows.EntityWorkflowParams[*demov1.DemoEngineState]{
		EntityId:     "test-query-multi-ops",
		InitialState: ApplyBusinessDefaults(nil),
	}

	// Start the workflow
	s.T().Log("🚀 Starting workflow for multiple operations test")
	workflowRun, err := s.client.ExecuteWorkflow(context.Background(), client.StartWorkflowOptions{
		ID:        "test-query-multi-ops-workflow-" + time.Now().Format("20060102-150405-000"),
		TaskQueue: s.taskQueue,
	}, DemoEntityWorkflow, params)
	s.Require().NoError(err)
	s.T().Log("📋 Workflow started")

	// Give workflow a moment to initialize
	time.Sleep(300 * time.Millisecond)

	// First increment
	requestCtx1 := &entityv1.RequestContext{
		UserId:         "test-user",
		OrgId:          "test-org",
		TeamId:         "test-team",
		Environment:    "test",
		Tenant:         "test-tenant",
		IdempotencyKey: "inc-1",
		RequestTime:    timestamppb.Now(),
	}

	incrementSignal1 := &demov1.DemoEngineSignal{
		Signal: &demov1.DemoEngineSignal_Increment{
			Increment: &demov1.IncrementSignal{Amount: 3, Reason: "First increment", Timestamp: timestamppb.Now()},
		},
	}

	s.T().Log("⏰ Sending first increment")
	updateHandle1, err := s.client.UpdateWorkflow(context.Background(), client.UpdateWorkflowOptions{
		WorkflowID:   workflowRun.GetID(),
		RunID:        workflowRun.GetRunID(),
		UpdateName:   "processEvent",
		WaitForStage: client.WorkflowUpdateStageCompleted,
		Args:         []interface{}{requestCtx1, incrementSignal1},
	})
	s.Require().NoError(err)
	
	ctx1, cancel1 := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel1()
	
	var updateResult1 *demov1.DemoEngineState
	err = updateHandle1.Get(ctx1, &updateResult1)
	s.Require().NoError(err)
	s.T().Log("🎯 First increment completed!")

	// Second increment
	requestCtx2 := &entityv1.RequestContext{
		UserId:         "test-user",
		OrgId:          "test-org",
		TeamId:         "test-team",
		Environment:    "test",
		Tenant:         "test-tenant",
		IdempotencyKey: "inc-2",
		RequestTime:    timestamppb.Now(),
	}

	incrementSignal2 := &demov1.DemoEngineSignal{
		Signal: &demov1.DemoEngineSignal_Increment{
			Increment: &demov1.IncrementSignal{Amount: 7, Reason: "Second increment", Timestamp: timestamppb.Now()},
		},
	}

	s.T().Log("⏰ Sending second increment")
	updateHandle2, err := s.client.UpdateWorkflow(context.Background(), client.UpdateWorkflowOptions{
		WorkflowID:   workflowRun.GetID(),
		RunID:        workflowRun.GetRunID(),
		UpdateName:   "processEvent",
		WaitForStage: client.WorkflowUpdateStageCompleted,
		Args:         []interface{}{requestCtx2, incrementSignal2},
	})
	s.Require().NoError(err)
	
	ctx2, cancel2 := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel2()
	
	var updateResult2 *demov1.DemoEngineState
	err = updateHandle2.Get(ctx2, &updateResult2)
	s.Require().NoError(err)
	s.T().Log("🎯 Second increment completed!")

	// Reset
	requestCtx3 := &entityv1.RequestContext{
		UserId:         "test-user",
		OrgId:          "test-org",
		TeamId:         "test-team",
		Environment:    "test",
		Tenant:         "test-tenant",
		IdempotencyKey: "reset-1",
		RequestTime:    timestamppb.Now(),
	}

	resetSignal := &demov1.DemoEngineSignal{
		Signal: &demov1.DemoEngineSignal_Reset_{
			Reset_: &demov1.ResetSignal{Reason: "Test reset", Timestamp: timestamppb.Now()},
		},
	}

	s.T().Log("⏰ Sending reset")
	updateHandle3, err := s.client.UpdateWorkflow(context.Background(), client.UpdateWorkflowOptions{
		WorkflowID:   workflowRun.GetID(),
		RunID:        workflowRun.GetRunID(),
		UpdateName:   "processEvent",
		WaitForStage: client.WorkflowUpdateStageCompleted,
		Args:         []interface{}{requestCtx3, resetSignal},
	})
	s.Require().NoError(err)
	
	ctx3, cancel3 := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel3()
	
	var updateResult3 *demov1.DemoEngineState
	err = updateHandle3.Get(ctx3, &updateResult3)
	s.Require().NoError(err)
	s.T().Log("🎯 Reset completed!")
	
	// Final query to verify complete history after all operations
	s.T().Log("🔍 Querying final entity state")
	response, err := s.client.QueryWorkflow(context.Background(), workflowRun.GetID(), workflowRun.GetRunID(), "getEntityState")
	s.NoError(err)

	var finalResponse entityworkflows.EntityQueryResponse[*demov1.DemoEngineState]
	err = response.Get(&finalResponse)
	s.NoError(err)

	// Should have 3 events in history and next event ID should be 4 (after reset)
	// Note: currentValue is omitted when 0 due to omitempty JSON tag
	s.Contains(finalResponse.CurrentStateJSON, `"nextEventId":"4"`)
	// Verify the reset event is in history
	s.Contains(finalResponse.CurrentStateJSON, `"type":"COUNTER_EVENT_TYPE_RESET"`)
	
	s.T().Log("✅ Multiple operations test completed successfully!")
}

func (s *DemoEntityQueryIntegrationTestSuite) TestQueryPerformanceWithLargeHistory() {
	params := entityworkflows.EntityWorkflowParams[*demov1.DemoEngineState]{
		EntityId:     "test-query-large-history",
		InitialState: ApplyBusinessDefaults(nil),
	}

	// Start the workflow
	s.T().Log("🚀 Starting workflow for performance test")
	workflowRun, err := s.client.ExecuteWorkflow(context.Background(), client.StartWorkflowOptions{
		ID:        "test-query-performance-workflow-" + time.Now().Format("20060102-150405-000"),
		TaskQueue: s.taskQueue,
	}, DemoEntityWorkflow, params)
	s.Require().NoError(err)
	s.T().Log("📋 Workflow started")

	// Give workflow a moment to initialize
	time.Sleep(300 * time.Millisecond)

	// Create large history with sequential operations (reduced for test performance)
	numOperations := 10
	
	// Send updates sequentially
	for i := 0; i < numOperations; i++ {
		requestCtx := &entityv1.RequestContext{
			UserId:         "test-user",
			OrgId:          "test-org",
			TeamId:         "test-team",
			Environment:    "test",
			Tenant:         "test-tenant",
			IdempotencyKey: fmt.Sprintf("bulk-%d", i),
			RequestTime:    timestamppb.Now(),
		}

		incrementSignal := &demov1.DemoEngineSignal{
			Signal: &demov1.DemoEngineSignal_Increment{
				Increment: &demov1.IncrementSignal{
					Amount:    1,
					Reason:    "Bulk increment",
					Timestamp: timestamppb.Now(),
				},
			},
		}

		s.T().Logf("⏰ Sending bulk increment %d/%d", i+1, numOperations)
		updateHandle, err := s.client.UpdateWorkflow(context.Background(), client.UpdateWorkflowOptions{
			WorkflowID:   workflowRun.GetID(),
			RunID:        workflowRun.GetRunID(),
			UpdateName:   "processEvent",
			WaitForStage: client.WorkflowUpdateStageCompleted,
			Args:         []interface{}{requestCtx, incrementSignal},
		})
		s.Require().NoError(err)
		
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		
		var updateResult *demov1.DemoEngineState
		err = updateHandle.Get(ctx, &updateResult)
		s.Require().NoError(err)
	}
	
	s.T().Log("🎯 All bulk operations completed!")
	
	// Query with large state - measure performance
	s.T().Log("🔍 Testing query performance with large history")
	start := time.Now()
	response, err := s.client.QueryWorkflow(context.Background(), workflowRun.GetID(), workflowRun.GetRunID(), "getEntityState")
	queryDuration := time.Since(start)

	s.NoError(err)
	s.Less(queryDuration, 5*time.Second, "Query should complete within 5 seconds even with large history")

	var queryResponse entityworkflows.EntityQueryResponse[*demov1.DemoEngineState]
	err = response.Get(&queryResponse)
	s.NoError(err)

	// Verify final state
	expectedValue := int64(numOperations)
	s.Contains(queryResponse.CurrentStateJSON, `"currentValue":"`+fmt.Sprintf("%d", expectedValue)+`"`)
	s.True(len(queryResponse.CurrentStateJSON) > 200, "JSON should be substantial with history")
	
	s.T().Logf("📊 Query performance: %v for state with %d operations", queryDuration, numOperations)
	s.T().Log("✅ Performance test completed successfully!")
}

func (s *DemoEntityQueryIntegrationTestSuite) TestQueryWithIdempotentOperations() {
	params := entityworkflows.EntityWorkflowParams[*demov1.DemoEngineState]{
		EntityId:     "test-query-idempotent",
		InitialState: ApplyBusinessDefaults(nil),
	}

	// Start the workflow
	s.T().Log("🚀 Starting workflow for idempotency test")
	workflowRun, err := s.client.ExecuteWorkflow(context.Background(), client.StartWorkflowOptions{
		ID:        "test-query-idempotent-workflow-" + time.Now().Format("20060102-150405-000"),
		TaskQueue: s.taskQueue,
	}, DemoEntityWorkflow, params)
	s.Require().NoError(err)
	s.T().Log("📋 Workflow started")

	// Give workflow a moment to initialize
	time.Sleep(300 * time.Millisecond)

	requestCtx := &entityv1.RequestContext{
		UserId:         "test-user",
		OrgId:          "test-org",
		TeamId:         "test-team",
		Environment:    "test",
		Tenant:         "test-tenant",
		IdempotencyKey: "duplicate-test",
		RequestTime:    timestamppb.Now(),
	}

	incrementSignal := &demov1.DemoEngineSignal{
		Signal: &demov1.DemoEngineSignal_Increment{
			Increment: &demov1.IncrementSignal{
				Amount:    10,
				Reason:    "Idempotent test",
				Timestamp: timestamppb.Now(),
			},
		},
	}

	// First update
	s.T().Log("⏰ Sending first update")
	updateHandle1, err := s.client.UpdateWorkflow(context.Background(), client.UpdateWorkflowOptions{
		WorkflowID:   workflowRun.GetID(),
		RunID:        workflowRun.GetRunID(),
		UpdateName:   "processEvent",
		WaitForStage: client.WorkflowUpdateStageCompleted,
		Args:         []interface{}{requestCtx, incrementSignal},
	})
	s.Require().NoError(err)
	
	ctx1, cancel1 := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel1()
	
	var updateResult1 *demov1.DemoEngineState
	err = updateHandle1.Get(ctx1, &updateResult1)
	s.Require().NoError(err)
	s.T().Log("🎯 First update completed!")

	// Second update with same idempotency key (should be deduplicated)
	requestCtx2 := &entityv1.RequestContext{
		UserId:         "test-user",
		OrgId:          "test-org",
		TeamId:         "test-team",
		Environment:    "test",
		Tenant:         "test-tenant",
		IdempotencyKey: "duplicate-test", // Same idempotency key
		RequestTime:    timestamppb.Now(),
	}

	incrementSignal2 := &demov1.DemoEngineSignal{
		Signal: &demov1.DemoEngineSignal_Increment{
			Increment: &demov1.IncrementSignal{
				Amount:    10,
				Reason:    "Idempotent test",
				Timestamp: timestamppb.Now(),
			},
		},
	}

	s.T().Log("⏰ Sending duplicate update (should be deduplicated)")
	updateHandle2, err := s.client.UpdateWorkflow(context.Background(), client.UpdateWorkflowOptions{
		WorkflowID:   workflowRun.GetID(),
		RunID:        workflowRun.GetRunID(),
		UpdateName:   "processEvent",
		WaitForStage: client.WorkflowUpdateStageCompleted,
		Args:         []interface{}{requestCtx2, incrementSignal2},
	})
	s.Require().NoError(err)
	
	ctx2, cancel2 := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel2()
	
	var updateResult2 *demov1.DemoEngineState
	err = updateHandle2.Get(ctx2, &updateResult2)
	s.Require().NoError(err)
	s.T().Log("🎯 Second update completed!")
	
	// Query final state after both operations
	s.T().Log("🔍 Querying final state after idempotent operations")
	response, err := s.client.QueryWorkflow(context.Background(), workflowRun.GetID(), workflowRun.GetRunID(), "getEntityState")
	s.NoError(err)

	var queryResponse entityworkflows.EntityQueryResponse[*demov1.DemoEngineState]
	err = response.Get(&queryResponse)
	s.NoError(err)

	// Should only have processed once due to idempotency
	s.Contains(queryResponse.CurrentStateJSON, `"currentValue":"10"`)
	s.Contains(queryResponse.CurrentStateJSON, `"nextEventId":"2"`) // Only one event processed
	
	s.T().Log("✅ Idempotency test completed successfully!")
} 