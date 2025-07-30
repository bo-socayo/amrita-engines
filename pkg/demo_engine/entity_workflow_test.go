package demo_engine

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/worker"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/bo-socayo/amrita-engines/pkg/entityworkflows"
	demov1 "github.com/bo-socayo/amrita-engines/gen/engines/demo/v1"
	entityv1 "github.com/bo-socayo/amrita-engines/gen/entity/v1"
)

type DemoEntityWorkflowTestSuite struct {
	suite.Suite
	
	// Use in-memory dev server
	devServer  *testsuite.DevServer
	client     client.Client
	worker     worker.Worker
	taskQueue  string
}

func (s *DemoEntityWorkflowTestSuite) SetupSuite() {
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
	s.taskQueue = "demo-entity-test-suite-" + time.Now().Format("20060102-150405")
	
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
	
	s.T().Log("✅ Started in-memory Temporal dev server and configured worker (workflow has built-in activities)")
}

func (s *DemoEntityWorkflowTestSuite) TearDownSuite() {
	if s.worker != nil {
		s.worker.Stop()
	}
	if s.devServer != nil {
		err := s.devServer.Stop()
		s.Require().NoError(err)
		s.T().Log("✅ Stopped in-memory Temporal dev server")
	}
}

func (s *DemoEntityWorkflowTestSuite) SetupTest() {
	// No per-test setup needed for basic tests
}

func (s *DemoEntityWorkflowTestSuite) TearDownTest() {
	// No per-test cleanup needed for basic tests
}

func TestDemoEntityWorkflowTestSuite(t *testing.T) {
	suite.Run(t, new(DemoEntityWorkflowTestSuite))
}

func (s *DemoEntityWorkflowTestSuite) TestDemoEntityWorkflow_BasicIncrement() {
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
		EntityId:     "test-demo-1",
		InitialState: initialState,
	}

	// Start the workflow
	s.T().Log("🚀 Starting long-running entity workflow")
	workflowRun, err := s.client.ExecuteWorkflow(context.Background(), client.StartWorkflowOptions{
		ID:        "test-basic-increment-workflow-" + time.Now().Format("20060102-150405-000"),
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
				Reason:    "Test increment",
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
		IdempotencyKey: "update-increment-basic-1",
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
	
	s.T().Log("✅ Basic increment test completed successfully!")
}

func (s *DemoEntityWorkflowTestSuite) TestDemoEntityWorkflow_Reset() {
	// Create initial state with some value
	initialState := &demov1.DemoEngineState{
		CurrentValue: 42,
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

	params := entityworkflows.EntityWorkflowParams[*demov1.DemoEngineState]{
		EntityId:     "test-demo-2",
		InitialState: initialState,
	}

	// Start the workflow
	s.T().Log("🚀 Starting workflow for reset test")
	workflowRun, err := s.client.ExecuteWorkflow(context.Background(), client.StartWorkflowOptions{
		ID:        "test-reset-workflow-" + time.Now().Format("20060102-150405-000"),
		TaskQueue: s.taskQueue,
	}, DemoEntityWorkflow, params)
	s.Require().NoError(err)
	s.T().Log("📋 Workflow started and ready for updates")

	// Give workflow a moment to initialize
	time.Sleep(300 * time.Millisecond)

	// Create reset signal
	resetSignal := &demov1.DemoEngineSignal{
		Signal: &demov1.DemoEngineSignal_Reset_{
			Reset_: &demov1.ResetSignal{
				Reason:    "Test reset",
				Timestamp: timestamppb.Now(),
			},
		},
	}

	// Create request context
	requestCtx := &entityv1.RequestContext{
		UserId:         "test-user-2",
		OrgId:          "test-org-2",
		TeamId:         "test-team-2",
		Environment:    "test",
		Tenant:         "test-tenant",
		IdempotencyKey: "update-reset-1",
		RequestTime:    timestamppb.Now(),
	}

	// Send update to the running workflow
	s.T().Log("⏰ Sending reset update via processEvent")
	updateHandle, err := s.client.UpdateWorkflow(context.Background(), client.UpdateWorkflowOptions{
		WorkflowID:   workflowRun.GetID(),
		RunID:        workflowRun.GetRunID(),
		UpdateName:   "processEvent",
		WaitForStage: client.WorkflowUpdateStageCompleted,
		Args:         []interface{}{requestCtx, resetSignal},
	})
	s.Require().NoError(err)
	
	// Get the update result with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	
	var updateResult *demov1.DemoEngineState
	err = updateHandle.Get(ctx, &updateResult)
	s.Require().NoError(err)
	s.T().Log("🎯 Reset update completed successfully!")
	
	// Verify the reset worked
	s.NotNil(updateResult)
	s.Equal(int64(0), updateResult.CurrentValue, "Value should be reset to 0")
	s.Len(updateResult.History, 1, "Should have one event in history")
	
	s.T().Logf("📊 Reset state - CurrentValue: %d, History length: %d", 
		updateResult.CurrentValue, len(updateResult.History))
	
	s.T().Log("✅ Reset test completed successfully!")
} 