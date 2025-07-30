package demo_engine

import (
	"testing"
	"time"

	"github.com/stretchr/testify/suite"
	"go.temporal.io/sdk/testsuite"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/bo-socayo/amrita-engines/pkg/entityworkflows"
	basev1 "github.com/bo-socayo/amrita-engines/gen/engines/base/v1"
	demov1 "github.com/bo-socayo/amrita-engines/gen/engines/demo/v1"
	entityv1 "github.com/bo-socayo/amrita-engines/gen/entity/v1"
)

type DemoEntityWorkflowUpdateTestSuite struct {
	suite.Suite
	testsuite.WorkflowTestSuite
	env *testsuite.TestWorkflowEnvironment
}

func (s *DemoEntityWorkflowUpdateTestSuite) SetupTest() {
	s.env = s.NewTestWorkflowEnvironment()
	
	// Register the entity workflow
	s.env.RegisterWorkflow(DemoEntityWorkflow)
}

func (s *DemoEntityWorkflowUpdateTestSuite) TearDownTest() {
	s.env.AssertExpectations(s.T())
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
	s.env.ExecuteWorkflow(DemoEntityWorkflow, params)

	// Wait for workflow to initialize then send update
	var updateResult *demov1.DemoEngineState
	s.env.RegisterDelayedCallback(func() {
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
			IdempotencyKey: "update-increment-1",
			RequestTime:    timestamppb.Now(),
		}

		// Send update and capture result
		s.env.UpdateWorkflow("processEvent", "increment-update-1", &testsuite.TestUpdateCallback{
			OnAccept: func() {
				// Update was accepted by validator
			},
			OnReject: func(err error) {
				s.Fail("Update should not be rejected", err)
			},
			OnComplete: func(response interface{}, err error) {
				s.NoError(err)
				updateResult = response.(*demov1.DemoEngineState)
				s.Equal(int64(5), updateResult.CurrentValue)
				s.Len(updateResult.History, 1)
				s.Equal("Test increment via update", updateResult.History[0].Reason)
				s.Equal(demov1.CounterEventType_COUNTER_EVENT_TYPE_INCREMENT, updateResult.History[0].Type)
			},
		}, requestCtx, incrementSignal)
	}, time.Millisecond*100)

	// Entity workflows are long-running and don't complete naturally
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

	s.env.ExecuteWorkflow(DemoEntityWorkflow, params)

	// Send reset update
	var updateResult *demov1.DemoEngineState
	s.env.RegisterDelayedCallback(func() {
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
			IdempotencyKey: "update-reset-1",
			RequestTime:    timestamppb.Now(),
		}

		s.env.UpdateWorkflow("processEvent", "reset-update-1", &testsuite.TestUpdateCallback{
			OnComplete: func(response interface{}, err error) {
				s.NoError(err)
				updateResult = response.(*demov1.DemoEngineState)
				s.Equal(int64(0), updateResult.CurrentValue)
				s.Len(updateResult.History, 1)
				s.Equal("Test reset via update", updateResult.History[0].Reason)
				s.Equal(demov1.CounterEventType_COUNTER_EVENT_TYPE_RESET, updateResult.History[0].Type)
			},
		}, requestCtx, resetSignal)
	}, time.Millisecond*100)

	// Entity workflows are long-running and don't complete naturally
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

	s.env.ExecuteWorkflow(DemoEntityWorkflow, params)

	// Send config update
	var updateResult *demov1.DemoEngineState
	s.env.RegisterDelayedCallback(func() {
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
			IdempotencyKey: "update-config-1",
			RequestTime:    timestamppb.Now(),
		}

		s.env.UpdateWorkflow("processEvent", "config-update-1", &testsuite.TestUpdateCallback{
			OnComplete: func(response interface{}, err error) {
				s.NoError(err)
				updateResult = response.(*demov1.DemoEngineState)
				s.Equal(int64(200), updateResult.Config.MaxValue)
				s.Equal(int64(5), updateResult.Config.DefaultIncrement)
				s.True(updateResult.Config.AllowNegative)
				s.Equal("Updated config via update handler", updateResult.Config.Description)
				s.Len(updateResult.History, 1)
				s.Equal(demov1.CounterEventType_COUNTER_EVENT_TYPE_CONFIG_UPDATE, updateResult.History[0].Type)
			},
		}, requestCtx, configUpdateSignal)
	}, time.Millisecond*100)

	// Entity workflows are long-running and don't complete naturally
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

	s.env.ExecuteWorkflow(DemoEntityWorkflow, params)

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
		IdempotencyKey: "same-idempotency-key", // Same key for both updates
		RequestTime:    timestamppb.Now(),
	}

	var firstResult, secondResult *demov1.DemoEngineState

	// First update with idempotency key
	s.env.RegisterDelayedCallback(func() {
		s.env.UpdateWorkflow("processEvent", "first-update", &testsuite.TestUpdateCallback{
			OnComplete: func(response interface{}, err error) {
				s.NoError(err)
				firstResult = response.(*demov1.DemoEngineState)
				s.Equal(int64(5), firstResult.CurrentValue)
				s.Len(firstResult.History, 1)
			},
		}, requestCtx, incrementSignal)
	}, time.Millisecond*100)

	// Second update with same idempotency key (should be deduplicated)
	s.env.RegisterDelayedCallback(func() {
		s.env.UpdateWorkflow("processEvent", "second-update", &testsuite.TestUpdateCallback{
			OnComplete: func(response interface{}, err error) {
				s.NoError(err)
				secondResult = response.(*demov1.DemoEngineState)
				// Should return same state, not increment again
				s.Equal(int64(5), secondResult.CurrentValue)
				s.Len(secondResult.History, 1) // Still only one event
			},
		}, requestCtx, incrementSignal)
	}, time.Millisecond*200)

	// Entity workflows are long-running and don't complete naturally
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

	s.env.ExecuteWorkflow(DemoEntityWorkflow, params)

	// First update: Increment by 3
	s.env.RegisterDelayedCallback(func() {
		incrementSignal := &demov1.DemoEngineSignal{
			Signal: &demov1.DemoEngineSignal_Increment{
				Increment: &demov1.IncrementSignal{
					Amount:    3,
					Reason:    "First increment",
					Timestamp: timestamppb.Now(),
				},
			},
		}

		requestCtx := &entityv1.RequestContext{
			UserId:         "test-user",
			OrgId:          "test-org",
			IdempotencyKey: "update-1",
			RequestTime:    timestamppb.Now(),
		}

		s.env.UpdateWorkflow("processEvent", "multi-update-1", &testsuite.TestUpdateCallback{
			OnComplete: func(response interface{}, err error) {
				s.NoError(err)
				result := response.(*demov1.DemoEngineState)
				s.Equal(int64(3), result.CurrentValue)
				s.Len(result.History, 1)
			},
		}, requestCtx, incrementSignal)
	}, time.Millisecond*100)

	// Second update: Increment by 7
	s.env.RegisterDelayedCallback(func() {
		incrementSignal := &demov1.DemoEngineSignal{
			Signal: &demov1.DemoEngineSignal_Increment{
				Increment: &demov1.IncrementSignal{
					Amount:    7,
					Reason:    "Second increment",
					Timestamp: timestamppb.Now(),
				},
			},
		}

		requestCtx := &entityv1.RequestContext{
			UserId:         "test-user",
			OrgId:          "test-org",
			IdempotencyKey: "update-2",
			RequestTime:    timestamppb.Now(),
		}

		s.env.UpdateWorkflow("processEvent", "multi-update-2", &testsuite.TestUpdateCallback{
			OnComplete: func(response interface{}, err error) {
				s.NoError(err)
				result := response.(*demov1.DemoEngineState)
				s.Equal(int64(10), result.CurrentValue) // 3 + 7 = 10
				s.Len(result.History, 2)
				s.Equal("Second increment", result.History[1].Reason)
			},
		}, requestCtx, incrementSignal)
	}, time.Millisecond*200)

	// Third update: Reset
	s.env.RegisterDelayedCallback(func() {
		resetSignal := &demov1.DemoEngineSignal{
			Signal: &demov1.DemoEngineSignal_Reset_{
				Reset_: &demov1.ResetSignal{
					Reason:    "Reset after increments",
					Timestamp: timestamppb.Now(),
				},
			},
		}

		requestCtx := &entityv1.RequestContext{
			UserId:         "test-user",
			OrgId:          "test-org",
			IdempotencyKey: "update-3",
			RequestTime:    timestamppb.Now(),
		}

		s.env.UpdateWorkflow("processEvent", "multi-update-3", &testsuite.TestUpdateCallback{
			OnComplete: func(response interface{}, err error) {
				s.NoError(err)
				result := response.(*demov1.DemoEngineState)
				s.Equal(int64(0), result.CurrentValue) // Reset to 0
				s.Len(result.History, 3)
				s.Equal("Reset after increments", result.History[2].Reason)
				s.Equal(demov1.CounterEventType_COUNTER_EVENT_TYPE_RESET, result.History[2].Type)
			},
		}, requestCtx, resetSignal)
	}, time.Millisecond*300)

	// Entity workflows are long-running and don't complete naturally
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

	s.env.ExecuteWorkflow(DemoEntityWorkflow, params)

	// Try to increment beyond max value
	s.env.RegisterDelayedCallback(func() {
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
			IdempotencyKey: "validation-test-1",
			RequestTime:    timestamppb.Now(),
		}

		s.env.UpdateWorkflow("processEvent", "validation-update", &testsuite.TestUpdateCallback{
			OnComplete: func(response interface{}, err error) {
				s.NoError(err)
				result := response.(*demov1.DemoEngineState)
				// Should be capped at max value
				s.Equal(int64(100), result.CurrentValue)
				s.Len(result.History, 1)
			},
		}, requestCtx, incrementSignal)
	}, time.Millisecond*100)

	// Entity workflows are long-running and don't complete naturally
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

	s.env.ExecuteWorkflow(DemoEntityWorkflow, params)

	// Send signal first
	s.env.RegisterDelayedCallback(func() {
		incrementSignal := &demov1.DemoEngineSignal{
			Signal: &demov1.DemoEngineSignal_Increment{
				Increment: &demov1.IncrementSignal{
					Amount:    3,
					Reason:    "Via signal",
					Timestamp: timestamppb.Now(),
				},
			},
		}

		envelope := &basev1.EventEnvelope{
			SequenceNumber: 1,
			EventId:        "signal-event-1",
			Timestamp:      timestamppb.Now(),
			Data:           mustMarshalAny(incrementSignal),
		}

		s.env.SignalWorkflow("process-event", envelope)
	}, time.Millisecond*100)

	// Send update after signal
	s.env.RegisterDelayedCallback(func() {
		incrementSignal := &demov1.DemoEngineSignal{
			Signal: &demov1.DemoEngineSignal_Increment{
				Increment: &demov1.IncrementSignal{
					Amount:    7,
					Reason:    "Via update",
					Timestamp: timestamppb.Now(),
				},
			},
		}

		requestCtx := &entityv1.RequestContext{
			UserId:         "test-user",
			OrgId:          "test-org",
			IdempotencyKey: "mixed-update-1",
			RequestTime:    timestamppb.Now(),
		}

		s.env.UpdateWorkflow("processEvent", "mixed-update", &testsuite.TestUpdateCallback{
			OnComplete: func(response interface{}, err error) {
				s.NoError(err)
				result := response.(*demov1.DemoEngineState)
				// Should have both signal and update results
				s.Equal(int64(10), result.CurrentValue) // 3 + 7 = 10
				s.Len(result.History, 2)
				
				// Verify both events are recorded
				reasons := []string{result.History[0].Reason, result.History[1].Reason}
				s.Contains(reasons, "Via signal")
				s.Contains(reasons, "Via update")
			},
		}, requestCtx, incrementSignal)
	}, time.Millisecond*200)

	// Entity workflows are long-running - we only test the update processing, not completion
}

// Note: mustMarshalAny is defined in entity_workflow_test.go 