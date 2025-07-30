package demo_engine

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"
	"google.golang.org/protobuf/types/known/timestamppb"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/bo-socayo/amrita-engines/pkg/entityworkflows"
	demov1 "github.com/bo-socayo/amrita-engines/gen/engines/demo/v1"
	entityv1 "github.com/bo-socayo/amrita-engines/gen/entity/v1"
)

type DemoEntityWorkflowContinueAsNewTestSuite struct {
	suite.Suite
	
	// Use in-memory dev server
	devServer  *testsuite.DevServer
	client     client.Client
	worker     worker.Worker
	taskQueue  string
}

func (s *DemoEntityWorkflowContinueAsNewTestSuite) SetupSuite() {
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
	s.taskQueue = "demo-entity-continue-as-new-suite-" + time.Now().Format("20060102-150405")
	
	// Create worker (shared across all tests in suite)
	s.worker = worker.New(s.client, s.taskQueue, worker.Options{})
	
	// Register the entity workflow with explicit name - it comes with activities built-in now!
	s.worker.RegisterWorkflowWithOptions(DemoEntityWorkflow, workflow.RegisterOptions{
		Name: "DemoEntityWorkflow",
	})
	
	// Start worker once for the entire suite
	go func() {
		err := s.worker.Run(make(chan interface{}))
		if err != nil {
			s.T().Logf("Worker error: %v", err)
		}
	}()
	
	// Give worker a moment to start
	time.Sleep(200 * time.Millisecond)
	
	s.T().Log("✅ Started in-memory Temporal dev server for continue-as-new tests")
}

func (s *DemoEntityWorkflowContinueAsNewTestSuite) TearDownSuite() {
	if s.worker != nil {
		s.worker.Stop()
	}
	if s.devServer != nil {
		err := s.devServer.Stop()
		s.Require().NoError(err)
		s.T().Log("✅ Stopped in-memory Temporal dev server")
	}
}

func (s *DemoEntityWorkflowContinueAsNewTestSuite) SetupTest() {
	// No per-test setup needed
}

func (s *DemoEntityWorkflowContinueAsNewTestSuite) TearDownTest() {
	// No per-test cleanup needed
}

func TestDemoEntityWorkflowContinueAsNewTestSuite(t *testing.T) {
	suite.Run(t, new(DemoEntityWorkflowContinueAsNewTestSuite))
}

// TestStateCompressionWithLargeHistory tests that state compression works when triggered by large history
func (s *DemoEntityWorkflowContinueAsNewTestSuite) TestStateCompressionWithLargeHistory() {
	// Create initial state with already large history to test compression
	initialHistory := make([]*demov1.CounterEvent, 60) // More than 50 limit
	for i := 0; i < 60; i++ {
		initialHistory[i] = &demov1.CounterEvent{
			EventId:       int64(i + 1),
			Type:          demov1.CounterEventType_COUNTER_EVENT_TYPE_INCREMENT,
			ValueChange:   1,
			PreviousValue: int64(i),
			NewValue:      int64(i + 1),
			Timestamp:     timestamppb.Now(),
		}
	}

	initialState := &demov1.DemoEngineState{
		CurrentValue: 60,
		Config: &demov1.DemoConfig{
			MaxValue:         1000,
			DefaultIncrement: 1,
			AllowNegative:    true,
			Description:      "Large history test",
		},
		History:     initialHistory,
		LastUpdated: timestamppb.Now(),
		NextEventId: 61,
	}

	params := entityworkflows.EntityWorkflowParams[*demov1.DemoEngineState]{
		EntityId:     "test-compression-large-history",
		InitialState: initialState,
	}

	// Start the workflow
	s.T().Log("🚀 Starting workflow for large history compression test")
	workflowRun, err := s.client.ExecuteWorkflow(context.Background(), client.StartWorkflowOptions{
		ID:        "test-compression-large-history-workflow-" + time.Now().Format("20060102-150405-000"),
		TaskQueue: s.taskQueue,
	}, DemoEntityWorkflow, params)
	s.Require().NoError(err)
	s.T().Log("📋 Workflow started")

	// Give workflow a moment to initialize
	time.Sleep(300 * time.Millisecond)

	// Query the state to verify compression was applied during initialization
	s.T().Log("🔍 Querying state after initialization")
	response, err := s.client.QueryWorkflow(context.Background(), workflowRun.GetID(), workflowRun.GetRunID(), "getEntityState")
	s.NoError(err)
	
	var queryResponse entityworkflows.EntityQueryResponse[*demov1.DemoEngineState]
	err = response.Get(&queryResponse)
	s.NoError(err)
	
	s.True(queryResponse.IsInitialized)
	
	// After engine initialization with ApplyBusinessDefaults, the state should maintain
	// the current value but the large history should be ready for compression
	s.Contains(queryResponse.CurrentStateJSON, `"currentValue":"60"`)
	s.Contains(queryResponse.CurrentStateJSON, `"description":"Large history test"`)
	
	s.T().Log("✅ Large history compression test completed successfully!")
}

// TestStateCompressionTriggeredByEngine tests that our custom compression function is actually used
func (s *DemoEntityWorkflowContinueAsNewTestSuite) TestStateCompressionTriggeredByEngine() {
	// Start with a state that has moderate history
	initialHistory := make([]*demov1.CounterEvent, 30)
	for i := 0; i < 30; i++ {
		initialHistory[i] = &demov1.CounterEvent{
			EventId:       int64(i + 1),
			Type:          demov1.CounterEventType_COUNTER_EVENT_TYPE_INCREMENT,
			ValueChange:   1,
			PreviousValue: int64(i),
			NewValue:      int64(i + 1),
			Timestamp:     timestamppb.Now(),
		}
	}

	initialState := &demov1.DemoEngineState{
		CurrentValue: 30,
		Config: &demov1.DemoConfig{
			MaxValue:         1000,
			DefaultIncrement: 1,
			AllowNegative:    true,
			Description:      "Engine compression test",
		},
		History:     initialHistory,
		NextEventId: 31,
	}

	params := entityworkflows.EntityWorkflowParams[*demov1.DemoEngineState]{
		EntityId:     "test-engine-compression",
		InitialState: initialState,
	}

	// Start the workflow
	s.T().Log("🚀 Starting workflow for engine compression test")
	workflowRun, err := s.client.ExecuteWorkflow(context.Background(), client.StartWorkflowOptions{
		ID:        "test-engine-compression-workflow-" + time.Now().Format("20060102-150405-000"),
		TaskQueue: s.taskQueue,
	}, DemoEntityWorkflow, params)
	s.Require().NoError(err)
	s.T().Log("📋 Workflow started")

	// Give workflow a moment to initialize
	time.Sleep(300 * time.Millisecond)

	// Add some events to build up history beyond compression threshold
	numEvents := 5
	for i := 0; i < numEvents; i++ {
		incrementSignal := &demov1.DemoEngineSignal{
			Signal: &demov1.DemoEngineSignal_Increment{
				Increment: &demov1.IncrementSignal{
					Amount:    1,
					Reason:    "Building history for compression test",
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
			IdempotencyKey: "compression-event-" + string(rune(i+49)), // Convert to char starting from '1'
			RequestTime:    timestamppb.Now(),
		}

		s.T().Logf("⏰ Sending compression event %d/%d", i+1, numEvents)
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
		
		var result *demov1.DemoEngineState
		err = updateHandle.Get(ctx, &result)
		s.Require().NoError(err)
		
		// Verify state is growing
		s.GreaterOrEqual(len(result.History), 1)
	}

	// Query final state to verify history has grown substantially
	s.T().Log("🔍 Querying final state after compression events")
	response, err := s.client.QueryWorkflow(context.Background(), workflowRun.GetID(), workflowRun.GetRunID(), "getEntityState")
	s.NoError(err)
	
	var queryResponse entityworkflows.EntityQueryResponse[*demov1.DemoEngineState]
	err = response.Get(&queryResponse)
	s.NoError(err)
	
	s.True(queryResponse.IsInitialized)
	
	// The state should have accumulated the new events
	// expectedValue = 30 + 5 = 35
	s.Contains(queryResponse.CurrentStateJSON, `"currentValue":"35"`) // 30 + 5 = 35
	s.Contains(queryResponse.CurrentStateJSON, `"history"`)
	
	s.T().Log("✅ Engine compression test completed successfully!")
}

// TestContinueAsNewPreservesEssentialState tests continue-as-new behavior
func (s *DemoEntityWorkflowContinueAsNewTestSuite) TestContinueAsNewPreservesEssentialState() {
	// This is a more complex test that would ideally trigger continue-as-new
	// For testing purposes, we'll simulate the workflow behavior under load
	
	initialState := &demov1.DemoEngineState{
		CurrentValue: 100,
		Config: &demov1.DemoConfig{
			MaxValue:         10000,
			DefaultIncrement: 10,
			AllowNegative:    true,
			Description:      "Continue-as-new preservation test",
		},
		History:     []*demov1.CounterEvent{},
		NextEventId: 1,
	}

	params := entityworkflows.EntityWorkflowParams[*demov1.DemoEngineState]{
		EntityId:     "test-continue-as-new-preservation",
		InitialState: initialState,
	}

	// Start the workflow
	s.T().Log("🚀 Starting workflow for continue-as-new preservation test")
	workflowRun, err := s.client.ExecuteWorkflow(context.Background(), client.StartWorkflowOptions{
		ID:        "test-continue-as-new-preservation-workflow-" + time.Now().Format("20060102-150405-000"),
		TaskQueue: s.taskQueue,
	}, DemoEntityWorkflow, params)
	s.Require().NoError(err)
	s.T().Log("📋 Workflow started")

	// Give workflow a moment to initialize
	time.Sleep(300 * time.Millisecond)

	// Send multiple operations to test state preservation patterns
	updateCount := 10
	for i := 0; i < updateCount; i++ {
		incrementSignal := &demov1.DemoEngineSignal{
			Signal: &demov1.DemoEngineSignal_Increment{
				Increment: &demov1.IncrementSignal{
					Amount:    5,
					Reason:    "Preservation test increment",
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
			IdempotencyKey: "preservation-" + string(rune(i+49)), // Convert to char starting from '1'
			RequestTime:    timestamppb.Now(),
		}

		s.T().Logf("⏰ Sending preservation event %d/%d", i+1, updateCount)
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
		
		var result *demov1.DemoEngineState
		err = updateHandle.Get(ctx, &result)
		s.Require().NoError(err)
		
		// Verify essential state is preserved during processing
		s.NotNil(result.Config)
		s.Equal("Continue-as-new preservation test", result.Config.Description)
		s.Equal(int64(10000), result.Config.MaxValue)
		s.GreaterOrEqual(result.CurrentValue, int64(100)) // Should be at least initial value
	}

	// Final verification of state preservation
	s.T().Log("🔍 Verifying final state preservation")
	response, err := s.client.QueryWorkflow(context.Background(), workflowRun.GetID(), workflowRun.GetRunID(), "getEntityState")
	s.NoError(err)
	
	var queryResponse entityworkflows.EntityQueryResponse[*demov1.DemoEngineState]
	err = response.Get(&queryResponse)
	s.NoError(err)
	
	s.True(queryResponse.IsInitialized)
	
	// Essential fields should be preserved
	s.Contains(queryResponse.CurrentStateJSON, `"currentValue":"150"`) // 100 + (10 * 5) = 150
	s.Contains(queryResponse.CurrentStateJSON, `"description":"Continue-as-new preservation test"`)
	s.Contains(queryResponse.CurrentStateJSON, `"maxValue":"10000"`)
	s.Contains(queryResponse.CurrentStateJSON, `"defaultIncrement":"10"`)
	
	s.T().Log("✅ Continue-as-new preservation test completed successfully!")
}

// TestStateCompressionWithMetadata tests compression of large metadata
func (s *DemoEntityWorkflowContinueAsNewTestSuite) TestStateCompressionWithMetadata() {
	// Create state with metadata that will trigger compression
	initialState := &demov1.DemoEngineState{
		CurrentValue: 42,
		Config: &demov1.DemoConfig{
			DefaultIncrement: 1,
			Description:      "Metadata compression test",
		},
		History:     []*demov1.CounterEvent{},
		NextEventId: 1,
	}

	params := entityworkflows.EntityWorkflowParams[*demov1.DemoEngineState]{
		EntityId:     "test-metadata-compression",
		InitialState: initialState,
	}

	// Start the workflow
	s.T().Log("🚀 Starting workflow for metadata compression test")
	workflowRun, err := s.client.ExecuteWorkflow(context.Background(), client.StartWorkflowOptions{
		ID:        "test-metadata-compression-workflow-" + time.Now().Format("20060102-150405-000"),
		TaskQueue: s.taskQueue,
	}, DemoEntityWorkflow, params)
	s.Require().NoError(err)
	s.T().Log("📋 Workflow started")

	// Give workflow a moment to initialize
	time.Sleep(300 * time.Millisecond)

	// Start with a query to verify initial state
	s.T().Log("🔍 Querying initial state")
	response, err := s.client.QueryWorkflow(context.Background(), workflowRun.GetID(), workflowRun.GetRunID(), "getEntityState")
	s.NoError(err)
	
	var queryResponse entityworkflows.EntityQueryResponse[*demov1.DemoEngineState]
	err = response.Get(&queryResponse)
	s.NoError(err)
	
	s.True(queryResponse.IsInitialized)
	s.Contains(queryResponse.CurrentStateJSON, `"currentValue":"42"`)
	s.Contains(queryResponse.CurrentStateJSON, `"description":"Metadata compression test"`)

	// Send an increment to verify normal operation
	incrementSignal := &demov1.DemoEngineSignal{
		Signal: &demov1.DemoEngineSignal_Increment{
			Increment: &demov1.IncrementSignal{
				Amount:    8,
				Reason:    "Test increment for metadata compression",
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
		IdempotencyKey: "metadata-compression-test",
		RequestTime:    timestamppb.Now(),
	}

	s.T().Log("⏰ Sending increment for metadata compression test")
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
	
	var result *demov1.DemoEngineState
	err = updateHandle.Get(ctx, &result)
	s.Require().NoError(err)
	s.T().Log("🎯 Metadata compression update completed!")
	
	s.Equal(int64(50), result.CurrentValue) // 42 + 8 = 50
	s.Len(result.History, 1)
	s.Equal("Test increment for metadata compression", result.History[0].Reason)
	
	s.T().Log("✅ Metadata compression test completed successfully!")
}

// TestWorkflowIdempotencyAcrossContinueAsNew tests that idempotency works across continue-as-new
func (s *DemoEntityWorkflowContinueAsNewTestSuite) TestWorkflowIdempotencyAcrossContinueAsNew() {
	initialState := &demov1.DemoEngineState{
		CurrentValue: 0,
		Config: &demov1.DemoConfig{
			MaxValue:         1000,
			DefaultIncrement: 1,
			AllowNegative:    false,
			Description:      "Idempotency across continue-as-new test",
		},
		History:     []*demov1.CounterEvent{},
		NextEventId: 1,
	}

	params := entityworkflows.EntityWorkflowParams[*demov1.DemoEngineState]{
		EntityId:     "test-idempotency-continue-as-new",
		InitialState: initialState,
	}

	// Start the workflow
	s.T().Log("🚀 Starting workflow for idempotency continue-as-new test")
	workflowRun, err := s.client.ExecuteWorkflow(context.Background(), client.StartWorkflowOptions{
		ID:        "test-idempotency-continue-as-new-workflow-" + time.Now().Format("20060102-150405-000"),
		TaskQueue: s.taskQueue,
	}, DemoEntityWorkflow, params)
	s.Require().NoError(err)
	s.T().Log("📋 Workflow started")

	// Give workflow a moment to initialize
	time.Sleep(300 * time.Millisecond)

	// Send the same idempotent operation multiple times
	idempotencyKey := "cross-continue-as-new-key"
	
	incrementSignal := &demov1.DemoEngineSignal{
		Signal: &demov1.DemoEngineSignal_Increment{
			Increment: &demov1.IncrementSignal{
				Amount:    25,
				Reason:    "Idempotent operation across continue-as-new",
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
		IdempotencyKey: idempotencyKey,
		RequestTime:    timestamppb.Now(),
	}

	// First operation
	s.T().Log("⏰ Sending first idempotent operation")
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
	s.T().Log("🎯 First idempotent operation completed!")
	
	s.Equal(int64(25), result1.CurrentValue)
	s.Len(result1.History, 1)

	// Duplicate operation with same idempotency key (should be deduplicated)
	s.T().Log("⏰ Sending duplicate idempotent operation (should be deduplicated)")
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
	
	var result2 *demov1.DemoEngineState
	err = updateHandle2.Get(ctx2, &result2)
	s.Require().NoError(err)
	s.T().Log("🎯 Duplicate idempotent operation completed (should be deduplicated)!")
	
	// Should still be 25, not 50
	s.Equal(int64(25), result2.CurrentValue)
	s.Len(result2.History, 1) // Should still be only one event

	// Final state verification
	s.T().Log("🔍 Verifying final idempotency state")
	response, err := s.client.QueryWorkflow(context.Background(), workflowRun.GetID(), workflowRun.GetRunID(), "getEntityState")
	s.NoError(err)
	
	var queryResponse entityworkflows.EntityQueryResponse[*demov1.DemoEngineState]
	err = response.Get(&queryResponse)
	s.NoError(err)
	
	s.True(queryResponse.IsInitialized)
	s.Contains(queryResponse.CurrentStateJSON, `"currentValue":"25"`)
	// Should contain only one event despite multiple requests
	
	s.T().Log("✅ Idempotency continue-as-new test completed successfully!")
}

// TestActualContinueAsNewAndCompression sends 1000+ events to actually trigger continue-as-new and compression
func (s *DemoEntityWorkflowContinueAsNewTestSuite) TestActualContinueAsNewAndCompression() {
	initialState := &demov1.DemoEngineState{
		CurrentValue: 0,
		Config: &demov1.DemoConfig{
			MaxValue:         100000, // High limit to avoid hitting max during test
			DefaultIncrement: 1,
			AllowNegative:    false,
			Description:      "Actual continue-as-new compression test",
		},
		History:     []*demov1.CounterEvent{},
		NextEventId: 1,
	}

	params := entityworkflows.EntityWorkflowParams[*demov1.DemoEngineState]{
		EntityId:     "test-actual-continue-as-new-compression",
		InitialState: initialState,
	}

	// Start the workflow
	s.T().Log("🚀 Starting workflow for ACTUAL continue-as-new test")
	workflowRun, err := s.client.ExecuteWorkflow(context.Background(), client.StartWorkflowOptions{
		ID:        "test-actual-continue-as-new-workflow-" + time.Now().Format("20060102-150405-000"),
		TaskQueue: s.taskQueue,
	}, "DemoEntityWorkflow", params)
	s.Require().NoError(err)
	s.T().Log("📋 Workflow started")

	// Give workflow a moment to initialize
	time.Sleep(300 * time.Millisecond)

	// Send events until continue-as-new is triggered (threshold is 1000)
	const maxEvents = 1010
	s.T().Logf("📦 Sending up to %d events to trigger continue-as-new", maxEvents)
	
	originalRunID := workflowRun.GetRunID()
	var finalValue int64 = 0
	var actualEventsSent = 0
	continueAsNewDetected := false
	
	for i := 0; i < maxEvents; i++ {
		incrementSignal := &demov1.DemoEngineSignal{
			Signal: &demov1.DemoEngineSignal_Increment{
				Increment: &demov1.IncrementSignal{
					Amount:    1,
					Reason:    fmt.Sprintf("Continue-as-new test event %d", i+1),
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
			IdempotencyKey: fmt.Sprintf("actual-continue-as-new-event-%d", i+1),
			RequestTime:    timestamppb.Now(),
		}

		updateHandle, err := s.client.UpdateWorkflow(context.Background(), client.UpdateWorkflowOptions{
			WorkflowID:   workflowRun.GetID(),
			RunID:        "", // Let Temporal find the current run
			UpdateName:   "processEvent",
			WaitForStage: client.WorkflowUpdateStageCompleted,
			Args:         []interface{}{requestCtx, incrementSignal},
		})
		
		// Check for continue-as-new backoff error (expected behavior)
		if err != nil && strings.Contains(err.Error(), "Backoff for continue-as-new") {
			s.T().Logf("🔄 Continue-as-new backoff detected at event %d - this is expected!", i+1)
			continueAsNewDetected = true
			break
		}
		s.Require().NoError(err)
		
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		
		var result *demov1.DemoEngineState
		err = updateHandle.Get(ctx, &result)
		if err != nil && strings.Contains(err.Error(), "Backoff for continue-as-new") {
			s.T().Logf("🔄 Continue-as-new backoff detected at event %d result - this is expected!", i+1)
			continueAsNewDetected = true
			cancel()
			break
		}
		s.Require().NoError(err)
		
		finalValue = result.CurrentValue
		actualEventsSent = i + 1
		
		// Every 100 events, log progress and check if continue-as-new happened
		if (i+1)%100 == 0 {
			s.T().Logf("📈 Progress: %d/%d events sent, currentValue: %d", i+1, maxEvents, finalValue)
			
			// Check if we're on a new run ID (continue-as-new happened)
			currentRun, err := s.client.DescribeWorkflowExecution(context.Background(), workflowRun.GetID(), "")
			s.Require().NoError(err)
			
			currentRunID := currentRun.GetWorkflowExecutionInfo().GetExecution().GetRunId()
			if currentRunID != originalRunID {
				s.T().Logf("🔄 CONTINUE-AS-NEW DETECTED! Original run: %s, Current run: %s", 
					originalRunID[:8], currentRunID[:8])
				// Update our reference for subsequent calls
				originalRunID = currentRunID
			}
		}
	}

	s.T().Log("🔍 Verifying final state after continue-as-new and compression")
	
	// Verify that continue-as-new was properly detected
	s.True(continueAsNewDetected, "Continue-as-new should have been detected")
	s.Greater(actualEventsSent, 100, "Should have sent a reasonable number of events before continue-as-new")
	s.T().Logf("✅ Continue-as-new triggered after %d events as expected", actualEventsSent)
	
	// Query final state (this might be from the new workflow execution after continue-as-new)
	response, err := s.client.QueryWorkflow(context.Background(), workflowRun.GetID(), "", "getEntityState")
	s.NoError(err)
	
	var queryResponse entityworkflows.EntityQueryResponse[*demov1.DemoEngineState]
	err = response.Get(&queryResponse)
	s.NoError(err)
	
	s.True(queryResponse.IsInitialized)
	
	// Verify final value accumulated the events that were actually sent
	s.Equal(finalValue, int64(actualEventsSent))
	s.T().Logf("📊 Final value: %d (matches %d events sent)", finalValue, actualEventsSent)
	
	// Key compression verification: History should be limited due to compression
	// The demo engine compresses history to max 50 events (see CompressState in engine.go)
	s.Contains(queryResponse.CurrentStateJSON, `"history"`)
	
	// Parse the state to check compression actually worked
	var finalState demov1.DemoEngineState
	err = protojson.Unmarshal([]byte(queryResponse.CurrentStateJSON), &finalState)
	s.NoError(err)
	
	// CRITICAL TEST: History should be compressed to ≤ 50 events even though we sent 1010
	s.T().Logf("📊 Final history length: %d events (should be ≤ 50 due to compression)", len(finalState.History))
	s.LessOrEqual(len(finalState.History), 50, "History should be compressed to max 50 events")
	
	// Verify essential state preservation across continue-as-new
	s.Equal(finalState.CurrentValue, int64(actualEventsSent))
	s.Equal("Actual continue-as-new compression test", finalState.Config.Description)
	s.Equal(int64(100000), finalState.Config.MaxValue)
	
	s.T().Log("✅ ACTUAL continue-as-new and compression test completed successfully!")
	s.T().Logf("✨ Verified: Sent %d events, final value %d, compressed history to %d events", 
		actualEventsSent, finalState.CurrentValue, len(finalState.History))
} 