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

type DemoEntityWorkflowUpdateTestSuite struct {
	suite.Suite
	
	// Use in-memory dev server
	devServer  *testsuite.DevServer
	client     client.Client
	worker     worker.Worker
	taskQueue  string
}

func (s *DemoEntityWorkflowUpdateTestSuite) SetupSuite() {
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
	s.taskQueue = "demo-entity-update-suite-" + time.Now().Format("20060102-150405")
	
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
	
	s.T().Log("✅ Started in-memory Temporal dev server for update tests")
}

func (s *DemoEntityWorkflowUpdateTestSuite) TearDownSuite() {
	if s.worker != nil {
		s.worker.Stop()
	}
	if s.devServer != nil {
		err := s.devServer.Stop()
		s.Require().NoError(err)
		s.T().Log("✅ Stopped in-memory Temporal dev server")
	}
}

func (s *DemoEntityWorkflowUpdateTestSuite) SetupTest() {
	// No per-test setup needed
}

func (s *DemoEntityWorkflowUpdateTestSuite) TearDownTest() {
	// No per-test cleanup needed
}

func TestDemoEntityWorkflowUpdateTestSuite(t *testing.T) {
	suite.Run(t, new(DemoEntityWorkflowUpdateTestSuite))
}

func (s *DemoEntityWorkflowUpdateTestSuite) Test_UpdateProcessEvent_IncrementSuccess() {
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
		EntityId:     "test-demo-update-1",
		InitialState: initialState,
	}

	// Start the workflow
	s.T().Log("🚀 Starting workflow for increment update test")
	workflowRun, err := s.client.ExecuteWorkflow(context.Background(), client.StartWorkflowOptions{
		ID:        "test-increment-update-workflow-" + time.Now().Format("20060102-150405-000"),
		TaskQueue: s.taskQueue,
	}, DemoEntityWorkflow, params)
	s.Require().NoError(err)
	s.T().Log("📋 Workflow started")

	// Give workflow a moment to initialize
	time.Sleep(300 * time.Millisecond)

	// Create increment signal
	incrementSignal := &demov1.DemoEngineSignal{
		Signal: &demov1.DemoEngineSignal_Increment{
			Increment: &demov1.IncrementSignal{
				Amount:    5,
				Reason:    "Test increment via update",
				Timestamp: timestamppb.Now(),
			},
		},
	}

	// Create request context for the update
	requestCtx := &entityv1.RequestContext{
		UserId:         "test-user",
		OrgId:          "test-org",
		TeamId:         "test-team",
		Environment:    "test",
		Tenant:         "test-tenant",
		IdempotencyKey: "update-increment-1",
		RequestTime:    timestamppb.Now(),
	}

	// Send update to the running workflow
	s.T().Log("⏰ Sending increment update via processEvent")
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
	s.T().Log("🎯 Increment update completed successfully!")
	
	// Verify the update worked
	s.NotNil(updateResult)
	s.Equal(int64(5), updateResult.CurrentValue)
	s.Len(updateResult.History, 1)
	s.Equal("Test increment via update", updateResult.History[0].Reason)
	s.Equal(demov1.CounterEventType_COUNTER_EVENT_TYPE_INCREMENT, updateResult.History[0].Type)
	
	s.T().Log("✅ Increment update test completed successfully!")
}

func (s *DemoEntityWorkflowUpdateTestSuite) Test_UpdateProcessEvent_ResetSuccess() {
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
		EntityId:     "test-demo-update-2",
		InitialState: initialState,
	}

	// Start the workflow
	s.T().Log("🚀 Starting workflow for reset update test")
	workflowRun, err := s.client.ExecuteWorkflow(context.Background(), client.StartWorkflowOptions{
		ID:        "test-reset-update-workflow-" + time.Now().Format("20060102-150405-000"),
		TaskQueue: s.taskQueue,
	}, DemoEntityWorkflow, params)
	s.Require().NoError(err)
	s.T().Log("📋 Workflow started")

	// Give workflow a moment to initialize
	time.Sleep(300 * time.Millisecond)

	// Create reset signal
	resetSignal := &demov1.DemoEngineSignal{
		Signal: &demov1.DemoEngineSignal_Reset_{
			Reset_: &demov1.ResetSignal{
				Reason:    "Test reset via update",
				Timestamp: timestamppb.Now(),
			},
		},
	}

	requestCtx := &entityv1.RequestContext{
		UserId:         "test-user",
		OrgId:          "test-org",
		TeamId:         "test-team",
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
	s.Equal(int64(0), updateResult.CurrentValue)
	s.Len(updateResult.History, 1)
	s.Equal("Test reset via update", updateResult.History[0].Reason)
	s.Equal(demov1.CounterEventType_COUNTER_EVENT_TYPE_RESET, updateResult.History[0].Type)
	
	s.T().Log("✅ Reset update test completed successfully!")
}

func (s *DemoEntityWorkflowUpdateTestSuite) Test_UpdateProcessEvent_ConfigUpdate() {
	initialState := &demov1.DemoEngineState{
		CurrentValue: 10,
		Config: &demov1.DemoConfig{
			MaxValue:         100,
			DefaultIncrement: 1,
			AllowNegative:    false,
			Description:      "Original config",
		},
		History:     []*demov1.CounterEvent{},
		LastUpdated: nil,
		NextEventId: 1,
	}

	params := entityworkflows.EntityWorkflowParams[*demov1.DemoEngineState]{
		EntityId:     "test-demo-update-config",
		InitialState: initialState,
	}

	// Start the workflow
	s.T().Log("🚀 Starting workflow for config update test")
	workflowRun, err := s.client.ExecuteWorkflow(context.Background(), client.StartWorkflowOptions{
		ID:        "test-config-update-workflow-" + time.Now().Format("20060102-150405-000"),
		TaskQueue: s.taskQueue,
	}, DemoEntityWorkflow, params)
	s.Require().NoError(err)
	s.T().Log("📋 Workflow started")

	// Give workflow a moment to initialize
	time.Sleep(300 * time.Millisecond)

	// Create config update signal
	newConfig := &demov1.DemoConfig{
		MaxValue:         200,
		DefaultIncrement: 5,
		AllowNegative:    true,
		Description:      "Updated config via update handler",
	}

	configUpdateSignal := &demov1.DemoEngineSignal{
		Signal: &demov1.DemoEngineSignal_UpdateConfig{
			UpdateConfig: &demov1.UpdateConfigSignal{
				Config:    newConfig,
				Reason:    "Test config update",
				Timestamp: timestamppb.Now(),
			},
		},
	}

	requestCtx := &entityv1.RequestContext{
		UserId:         "test-user",
		OrgId:          "test-org",
		TeamId:         "test-team",
		Environment:    "test",
		Tenant:         "test-tenant",
		IdempotencyKey: "update-config-1",
		RequestTime:    timestamppb.Now(),
	}

	// Send update to the running workflow
	s.T().Log("⏰ Sending config update via processEvent")
	updateHandle, err := s.client.UpdateWorkflow(context.Background(), client.UpdateWorkflowOptions{
		WorkflowID:   workflowRun.GetID(),
		RunID:        workflowRun.GetRunID(),
		UpdateName:   "processEvent",
		WaitForStage: client.WorkflowUpdateStageCompleted,
		Args:         []interface{}{requestCtx, configUpdateSignal},
	})
	s.Require().NoError(err)
	
	// Get the update result with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	
	var updateResult *demov1.DemoEngineState
	err = updateHandle.Get(ctx, &updateResult)
	s.Require().NoError(err)
	s.T().Log("🎯 Config update completed successfully!")
	
	// Verify the config update worked
	s.NotNil(updateResult)
	s.Equal(int64(200), updateResult.Config.MaxValue)
	s.Equal(int64(5), updateResult.Config.DefaultIncrement)
	s.True(updateResult.Config.AllowNegative)
	s.Equal("Updated config via update handler", updateResult.Config.Description)
	s.Len(updateResult.History, 1)
	s.Equal(demov1.CounterEventType_COUNTER_EVENT_TYPE_CONFIG_UPDATE, updateResult.History[0].Type)
	
	s.T().Log("✅ Config update test completed successfully!")
}

func (s *DemoEntityWorkflowUpdateTestSuite) Test_UpdateProcessEvent_IdempotencyHandling() {
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

	params := entityworkflows.EntityWorkflowParams[*demov1.DemoEngineState]{
		EntityId:     "test-demo-idempotency",
		InitialState: initialState,
	}

	// Start the workflow
	s.T().Log("🚀 Starting workflow for idempotency test")
	workflowRun, err := s.client.ExecuteWorkflow(context.Background(), client.StartWorkflowOptions{
		ID:        "test-idempotency-workflow-" + time.Now().Format("20060102-150405-000"),
		TaskQueue: s.taskQueue,
	}, DemoEntityWorkflow, params)
	s.Require().NoError(err)
	s.T().Log("📋 Workflow started")

	// Give workflow a moment to initialize
	time.Sleep(300 * time.Millisecond)

	incrementSignal := &demov1.DemoEngineSignal{
		Signal: &demov1.DemoEngineSignal_Increment{
			Increment: &demov1.IncrementSignal{
				Amount:    5,
				Reason:    "Idempotent increment",
				Timestamp: timestamppb.Now(),
			},
		},
	}

	requestCtx := &entityv1.RequestContext{
		UserId:         "test-user",
		OrgId:          "test-org",
		TeamId:         "test-team",
		Environment:    "test",
		Tenant:         "test-tenant",
		IdempotencyKey: "same-idempotency-key", // Same key for both updates
		RequestTime:    timestamppb.Now(),
	}

	// First update with idempotency key
	s.T().Log("⏰ Sending first update with idempotency key")
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
	
	var firstResult *demov1.DemoEngineState
	err = updateHandle1.Get(ctx1, &firstResult)
	s.Require().NoError(err)
	s.T().Log("🎯 First update completed!")
	
	s.Equal(int64(5), firstResult.CurrentValue)
	s.Len(firstResult.History, 1)

	// Second update with same idempotency key (should be deduplicated)
	s.T().Log("⏰ Sending second update with same idempotency key (should be deduplicated)")
	updateHandle2, err := s.client.UpdateWorkflow(context.Background(), client.UpdateWorkflowOptions{
		WorkflowID:   workflowRun.GetID(),
		RunID:        workflowRun.GetRunID(),
		UpdateName:   "processEvent",
		WaitForStage: client.WorkflowUpdateStageCompleted,
		Args:         []interface{}{requestCtx, incrementSignal},
	})
	s.Require().NoError(err)
	
	ctx2, cancel2 := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel2()
	
	var secondResult *demov1.DemoEngineState
	err = updateHandle2.Get(ctx2, &secondResult)
	s.Require().NoError(err)
	s.T().Log("🎯 Second update completed (should be deduplicated)!")
	
	// Should return same state, not increment again
	s.Equal(int64(5), secondResult.CurrentValue)
	s.Len(secondResult.History, 1) // Still only one event
	
	s.T().Log("✅ Idempotency test completed successfully!")
}

func (s *DemoEntityWorkflowUpdateTestSuite) Test_UpdateProcessEvent_MultipleUpdatesSequential() {
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

	params := entityworkflows.EntityWorkflowParams[*demov1.DemoEngineState]{
		EntityId:     "test-demo-multi-updates",
		InitialState: initialState,
	}

	// Start the workflow
	s.T().Log("🚀 Starting workflow for multiple sequential updates test")
	workflowRun, err := s.client.ExecuteWorkflow(context.Background(), client.StartWorkflowOptions{
		ID:        "test-multi-updates-workflow-" + time.Now().Format("20060102-150405-000"),
		TaskQueue: s.taskQueue,
	}, DemoEntityWorkflow, params)
	s.Require().NoError(err)
	s.T().Log("📋 Workflow started")

	// Give workflow a moment to initialize
	time.Sleep(300 * time.Millisecond)

	// First update: Increment by 3
	incrementSignal1 := &demov1.DemoEngineSignal{
		Signal: &demov1.DemoEngineSignal_Increment{
			Increment: &demov1.IncrementSignal{
				Amount:    3,
				Reason:    "First increment",
				Timestamp: timestamppb.Now(),
			},
		},
	}

	requestCtx1 := &entityv1.RequestContext{
		UserId:         "test-user",
		OrgId:          "test-org",
		TeamId:         "test-team",
		Environment:    "test",
		Tenant:         "test-tenant",
		IdempotencyKey: "update-1",
		RequestTime:    timestamppb.Now(),
	}

	s.T().Log("⏰ Sending first increment (3)")
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
	
	var result1 *demov1.DemoEngineState
	err = updateHandle1.Get(ctx1, &result1)
	s.Require().NoError(err)
	s.T().Log("🎯 First increment completed!")
	
	s.Equal(int64(3), result1.CurrentValue)
	s.Len(result1.History, 1)

	// Second update: Increment by 7
	incrementSignal2 := &demov1.DemoEngineSignal{
		Signal: &demov1.DemoEngineSignal_Increment{
			Increment: &demov1.IncrementSignal{
				Amount:    7,
				Reason:    "Second increment",
				Timestamp: timestamppb.Now(),
			},
		},
	}

	requestCtx2 := &entityv1.RequestContext{
		UserId:         "test-user",
		OrgId:          "test-org",
		TeamId:         "test-team",
		Environment:    "test",
		Tenant:         "test-tenant",
		IdempotencyKey: "update-2",
		RequestTime:    timestamppb.Now(),
	}

	s.T().Log("⏰ Sending second increment (7)")
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
	
	var result2 *demov1.DemoEngineState
	err = updateHandle2.Get(ctx2, &result2)
	s.Require().NoError(err)
	s.T().Log("🎯 Second increment completed!")
	
	s.Equal(int64(10), result2.CurrentValue) // 3 + 7 = 10
	s.Len(result2.History, 2)
	s.Equal("Second increment", result2.History[1].Reason)

	// Third update: Reset
	resetSignal := &demov1.DemoEngineSignal{
		Signal: &demov1.DemoEngineSignal_Reset_{
			Reset_: &demov1.ResetSignal{
				Reason:    "Reset after increments",
				Timestamp: timestamppb.Now(),
			},
		},
	}

	requestCtx3 := &entityv1.RequestContext{
		UserId:         "test-user",
		OrgId:          "test-org",
		TeamId:         "test-team",
		Environment:    "test",
		Tenant:         "test-tenant",
		IdempotencyKey: "update-3",
		RequestTime:    timestamppb.Now(),
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
	
	var result3 *demov1.DemoEngineState
	err = updateHandle3.Get(ctx3, &result3)
	s.Require().NoError(err)
	s.T().Log("🎯 Reset completed!")
	
	s.Equal(int64(0), result3.CurrentValue) // Reset to 0
	s.Len(result3.History, 3)
	s.Equal("Reset after increments", result3.History[2].Reason)
	s.Equal(demov1.CounterEventType_COUNTER_EVENT_TYPE_RESET, result3.History[2].Type)
	
	s.T().Log("✅ Multiple sequential updates test completed successfully!")
}

func (s *DemoEntityWorkflowUpdateTestSuite) Test_UpdateProcessEvent_ValidationBusinessRules() {
	// Create initial state near max value
	initialState := &demov1.DemoEngineState{
		CurrentValue: 95,
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
		EntityId:     "test-demo-validation",
		InitialState: initialState,
	}

	// Start the workflow
	s.T().Log("🚀 Starting workflow for validation test")
	workflowRun, err := s.client.ExecuteWorkflow(context.Background(), client.StartWorkflowOptions{
		ID:        "test-validation-workflow-" + time.Now().Format("20060102-150405-000"),
		TaskQueue: s.taskQueue,
	}, DemoEntityWorkflow, params)
	s.Require().NoError(err)
	s.T().Log("📋 Workflow started")

	// Give workflow a moment to initialize
	time.Sleep(300 * time.Millisecond)

	// Try to increment beyond max value
	incrementSignal := &demov1.DemoEngineSignal{
		Signal: &demov1.DemoEngineSignal_Increment{
			Increment: &demov1.IncrementSignal{
				Amount:    10, // This would make it 105, exceeding max of 100
				Reason:    "Test max validation",
				Timestamp: timestamppb.Now(),
			},
		},
	}

	requestCtx := &entityv1.RequestContext{
		UserId:         "test-user",
		OrgId:          "test-org",
		TeamId:         "test-team",
		Environment:    "test",
		Tenant:         "test-tenant",
		IdempotencyKey: "validation-test-1",
		RequestTime:    timestamppb.Now(),
	}

	s.T().Log("⏰ Sending increment that exceeds max value (should fail validation)")
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
	s.Require().Error(err) // Expect an error due to validation failure
	s.Contains(err.Error(), "increment would exceed max value")
	s.T().Log("🎯 Validation correctly failed as expected!")
	
	s.T().Log("✅ Validation test completed successfully!")
}

func (s *DemoEntityWorkflowUpdateTestSuite) Test_UpdateProcessEvent_CombineSignalsAndUpdates() {
	// Test that both signals and updates work together correctly
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

	params := entityworkflows.EntityWorkflowParams[*demov1.DemoEngineState]{
		EntityId:     "test-demo-mixed",
		InitialState: initialState,
	}

	// Start the workflow
	s.T().Log("🚀 Starting workflow for mixed signal and update test")
	workflowRun, err := s.client.ExecuteWorkflow(context.Background(), client.StartWorkflowOptions{
		ID:        "test-mixed-workflow-" + time.Now().Format("20060102-150405-000"),
		TaskQueue: s.taskQueue,
	}, DemoEntityWorkflow, params)
	s.Require().NoError(err)
	s.T().Log("📋 Workflow started")

	// Give workflow a moment to initialize
	time.Sleep(300 * time.Millisecond)

	// Send signal first
	s.T().Log("⏰ Sending increment signal via processEvent")
	incrementSignal := &demov1.DemoEngineSignal{
		Signal: &demov1.DemoEngineSignal_Increment{
			Increment: &demov1.IncrementSignal{
				Amount:    3,
				Reason:    "Via signal",
				Timestamp: timestamppb.Now(),
			},
		},
	}

	requestCtx := &entityv1.RequestContext{
		UserId:         "test-user",
		OrgId:          "test-org",
		TeamId:         "test-team",
		Environment:    "test",
		Tenant:         "test-tenant",
		IdempotencyKey: "mixed-update-1",
		RequestTime:    timestamppb.Now(),
	}

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
	
	var result1 *demov1.DemoEngineState
	err = updateHandle1.Get(ctx1, &result1)
	s.Require().NoError(err)
	s.T().Log("🎯 Signal update completed!")
	
	s.Equal(int64(3), result1.CurrentValue)
	s.Len(result1.History, 1)

	// Send update after signal
	s.T().Log("⏰ Sending increment update via processEvent")
	incrementSignal2 := &demov1.DemoEngineSignal{
		Signal: &demov1.DemoEngineSignal_Increment{
			Increment: &demov1.IncrementSignal{
				Amount:    7,
				Reason:    "Via update",
				Timestamp: timestamppb.Now(),
			},
		},
	}

	requestCtx2 := &entityv1.RequestContext{
		UserId:         "test-user",
		OrgId:          "test-org",
		TeamId:         "test-team",
		Environment:    "test",
		Tenant:         "test-tenant",
		IdempotencyKey: "mixed-update-2", // Different idempotency key for second operation
		RequestTime:    timestamppb.Now(),
	}

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
	
	var result2 *demov1.DemoEngineState
	err = updateHandle2.Get(ctx2, &result2)
	s.Require().NoError(err)
	s.T().Log("🎯 Update update completed!")
	
	s.Equal(int64(10), result2.CurrentValue) // 3 + 7 = 10
	s.Len(result2.History, 2)
	
	// Verify both events are recorded
	reasons := []string{result2.History[0].Reason, result2.History[1].Reason}
	s.Contains(reasons, "Via signal")
	s.Contains(reasons, "Via update")
	
	s.T().Log("✅ Mixed signal and update test completed successfully!")
} 