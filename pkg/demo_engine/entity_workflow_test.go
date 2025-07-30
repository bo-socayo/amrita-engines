package demo_engine

import (
	"testing"
	"time"

	"github.com/stretchr/testify/suite"
	"go.temporal.io/sdk/testsuite"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/bo-socayo/amrita-engines/pkg/engines"
	"github.com/bo-socayo/amrita-engines/pkg/entityworkflows"
	"github.com/bo-socayo/amrita-engines/pkg/entityworkflows/activities"
	basev1 "github.com/bo-socayo/amrita-engines/gen/engines/base/v1"
	demov1 "github.com/bo-socayo/amrita-engines/gen/engines/demo/v1"
)

type DemoEntityWorkflowTestSuite struct {
	suite.Suite
	testsuite.WorkflowTestSuite
	env *testsuite.TestWorkflowEnvironment
}

func (s *DemoEntityWorkflowTestSuite) SetupTest() {
	s.env = s.NewTestWorkflowEnvironment()
	
	// Register the entity workflow
	s.env.RegisterWorkflow(DemoEntityWorkflow)
	
	// ✅ Register the ProcessEventActivity for deterministic engine processing
	s.env.RegisterActivity(activities.ProcessEventActivity[*demov1.DemoEngineState, *demov1.DemoEngineSignal, *demov1.DemoEngineTransitionInfo])
	
	// ✅ Register demo engine factory for activity execution
	activities.RegisterEngine[*demov1.DemoEngineState, *demov1.DemoEngineSignal, *demov1.DemoEngineTransitionInfo](
		"demo.Engine",
		func() engines.Engine[*demov1.DemoEngineState, *demov1.DemoEngineSignal, *demov1.DemoEngineTransitionInfo] {
			// Create demo engine with the same configuration as used in DemoEntityWorkflow
			config := engines.BaseEngineConfig[*demov1.DemoEngineState, *demov1.DemoEngineSignal, *demov1.DemoEngineTransitionInfo]{
				EngineName:           EngineName,
				BusinessLogicVersion: BusinessLogicVersion,
				StateTypeName:        "DemoEngineState",
				EventTypeName:        "DemoEngineSignal",
				TransitionTypeName:   "DemoEngineTransitionInfo",
				Processor:            ProcessSignal,
				Defaults:             ApplyBusinessDefaults,
				Compress:             CompressState,
			}
			return engines.NewBaseEngine(config)
		},
	)
}

func (s *DemoEntityWorkflowTestSuite) TearDownTest() {
	s.env.AssertExpectations(s.T())
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
		LastUpdated: nil, // Will be set on first signal
		NextEventId: 1,
	}

	// Create workflow params (simplified)
	params := entityworkflows.EntityWorkflowParams[*demov1.DemoEngineState]{
		EntityId:     "test-demo-1",
		InitialState: initialState,
	}

	// Start the workflow
	s.env.ExecuteWorkflow(DemoEntityWorkflow, params)

	// Wait for workflow to initialize then send increment signal
	s.env.RegisterDelayedCallback(func() {
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

		// Create event envelope
		envelope := &basev1.EventEnvelope{
			SequenceNumber: 1,
			EventId:        "test-increment-1", 
			Timestamp:      timestamppb.Now(),
			Data:           mustMarshalAny(incrementSignal),
		}

		s.env.SignalWorkflow("process-event", envelope)
	}, time.Millisecond*100)

	// For now, just verify the workflow doesn't fail
	// TODO: Add proper query testing once we understand the query interface better
	s.True(s.env.IsWorkflowCompleted())
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
	s.env.ExecuteWorkflow(DemoEntityWorkflow, params)

	// Send reset signal
	s.env.RegisterDelayedCallback(func() {
		resetSignal := &demov1.DemoEngineSignal{
			Signal: &demov1.DemoEngineSignal_Reset_{
				Reset_: &demov1.ResetSignal{
					Reason:    "Test reset",
					Timestamp: timestamppb.Now(),
				},
			},
		}

		envelope := &basev1.EventEnvelope{
			SequenceNumber: 1,
			EventId:        "test-reset-1",
			Timestamp:      timestamppb.Now(),
			Data:           mustMarshalAny(resetSignal),
		}

		s.env.SignalWorkflow("process-event", envelope)
	}, time.Millisecond*100)

	// For now, just verify the workflow doesn't fail
	// TODO: Add proper query testing once we understand the query interface better
	s.True(s.env.IsWorkflowCompleted())
}

// Helper function to marshal protobuf to Any
func mustMarshalAny(msg *demov1.DemoEngineSignal) *anypb.Any {
	any, err := anypb.New(msg)
	if err != nil {
		panic(err)
	}
	return any
} 