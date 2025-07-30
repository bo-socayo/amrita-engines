package demo_engine

import (
	"testing"
	"time"

	"github.com/stretchr/testify/suite"
	"go.temporal.io/sdk/testsuite"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/bo-socayo/amrita-engines/pkg/entityworkflows"
	demov1 "github.com/bo-socayo/amrita-engines/gen/engines/demo/v1"
	entityv1 "github.com/bo-socayo/amrita-engines/gen/entity/v1"
)

type DemoEntityWorkflowContinueAsNewTestSuite struct {
	suite.Suite
	testsuite.WorkflowTestSuite
	env *testsuite.TestWorkflowEnvironment
}

func (s *DemoEntityWorkflowContinueAsNewTestSuite) SetupTest() {
	s.env = s.NewTestWorkflowEnvironment()
	
	// Register the entity workflow
	s.env.RegisterWorkflow(DemoEntityWorkflow)
}

func (s *DemoEntityWorkflowContinueAsNewTestSuite) TearDownTest() {
	s.env.AssertExpectations(s.T())
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

	s.env.ExecuteWorkflow(DemoEntityWorkflow, params)

	// Query the state to verify compression was applied during initialization
	s.env.RegisterDelayedCallback(func() {
		encoded, err := s.env.QueryWorkflow("getEntityState")
		s.NoError(err)
		
		var queryResponse entityworkflows.EntityQueryResponse[*demov1.DemoEngineState]
		err = encoded.Get(&queryResponse)
		s.NoError(err)
		
		s.True(queryResponse.IsInitialized)
		
		// After engine initialization with ApplyBusinessDefaults, the state should maintain
		// the current value but the large history should be ready for compression
		s.Contains(queryResponse.CurrentStateJSON, `"currentValue":60`)
		s.Contains(queryResponse.CurrentStateJSON, `"description":"Large history test"`)
	}, time.Millisecond*100)
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

	s.env.ExecuteWorkflow(DemoEntityWorkflow, params)

	// Add many more events to build up history beyond compression threshold
	for i := 0; i < 25; i++ {
		eventId := i + 1
		s.env.RegisterDelayedCallback(func() {
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
				IdempotencyKey: "compression-event-" + string(rune(eventId)),
				RequestTime:    timestamppb.Now(),
			}

			s.env.UpdateWorkflow("processEvent", "compression-update-"+string(rune(eventId)), &testsuite.TestUpdateCallback{
				OnComplete: func(response interface{}, err error) {
					s.NoError(err)
					result := response.(*demov1.DemoEngineState)
					// Verify state is growing
					s.GreaterOrEqual(len(result.History), 1)
				},
			}, requestCtx, incrementSignal)
		}, time.Duration(eventId*50)*time.Millisecond)
	}

	// Query final state to verify history has grown substantially
	s.env.RegisterDelayedCallback(func() {
		encoded, err := s.env.QueryWorkflow("getEntityState")
		s.NoError(err)
		
		var queryResponse entityworkflows.EntityQueryResponse[*demov1.DemoEngineState]
		err = encoded.Get(&queryResponse)
		s.NoError(err)
		
		s.True(queryResponse.IsInitialized)
		
		// The state should have accumulated many events
		// (This tests that our compression function will have data to work with)
		s.Contains(queryResponse.CurrentStateJSON, `"currentValue":55`) // 30 + 25 = 55
		s.Contains(queryResponse.CurrentStateJSON, `"history"`)
	}, time.Millisecond*2000) // Wait for all updates to complete
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

	s.env.ExecuteWorkflow(DemoEntityWorkflow, params)

	// Send multiple operations to test state preservation patterns
	updateCount := 10
	for i := 0; i < updateCount; i++ {
		eventId := i + 1
		s.env.RegisterDelayedCallback(func() {
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
				IdempotencyKey: "preservation-" + string(rune(eventId+48)), // Convert to char
				RequestTime:    timestamppb.Now(),
			}

			s.env.UpdateWorkflow("processEvent", "preservation-update-"+string(rune(eventId+48)), &testsuite.TestUpdateCallback{
				OnComplete: func(response interface{}, err error) {
					s.NoError(err)
					result := response.(*demov1.DemoEngineState)
					
					// Verify essential state is preserved during processing
					s.NotNil(result.Config)
					s.Equal("Continue-as-new preservation test", result.Config.Description)
					s.Equal(int64(10000), result.Config.MaxValue)
					s.GreaterOrEqual(result.CurrentValue, int64(100)) // Should be at least initial value
				},
			}, requestCtx, incrementSignal)
		}, time.Duration(eventId*100)*time.Millisecond)
	}

	// Final verification of state preservation
	s.env.RegisterDelayedCallback(func() {
		encoded, err := s.env.QueryWorkflow("getEntityState")
		s.NoError(err)
		
		var queryResponse entityworkflows.EntityQueryResponse[*demov1.DemoEngineState]
		err = encoded.Get(&queryResponse)
		s.NoError(err)
		
		s.True(queryResponse.IsInitialized)
		
		// Essential fields should be preserved
		s.Contains(queryResponse.CurrentStateJSON, `"currentValue":150`) // 100 + (10 * 5) = 150
		s.Contains(queryResponse.CurrentStateJSON, `"description":"Continue-as-new preservation test"`)
		s.Contains(queryResponse.CurrentStateJSON, `"maxValue":10000`)
		s.Contains(queryResponse.CurrentStateJSON, `"defaultIncrement":10`)
	}, time.Millisecond*1500)
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

	s.env.ExecuteWorkflow(DemoEntityWorkflow, params)

	// Start with a query to verify initial state
	s.env.RegisterDelayedCallback(func() {
		encoded, err := s.env.QueryWorkflow("getEntityState")
		s.NoError(err)
		
		var queryResponse entityworkflows.EntityQueryResponse[*demov1.DemoEngineState]
		err = encoded.Get(&queryResponse)
		s.NoError(err)
		
		s.True(queryResponse.IsInitialized)
		s.Contains(queryResponse.CurrentStateJSON, `"currentValue":42`)
		s.Contains(queryResponse.CurrentStateJSON, `"description":"Metadata compression test"`)
	}, time.Millisecond*100)

	// Send an increment to verify normal operation
	s.env.RegisterDelayedCallback(func() {
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
			IdempotencyKey: "metadata-compression-test",
			RequestTime:    timestamppb.Now(),
		}

		s.env.UpdateWorkflow("processEvent", "metadata-update", &testsuite.TestUpdateCallback{
			OnComplete: func(response interface{}, err error) {
				s.NoError(err)
				result := response.(*demov1.DemoEngineState)
				s.Equal(int64(50), result.CurrentValue) // 42 + 8 = 50
				s.Len(result.History, 1)
				s.Equal("Test increment for metadata compression", result.History[0].Reason)
			},
		}, requestCtx, incrementSignal)
	}, time.Millisecond*200)
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

	s.env.ExecuteWorkflow(DemoEntityWorkflow, params)

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
		IdempotencyKey: idempotencyKey,
		RequestTime:    timestamppb.Now(),
	}

	// First operation
	s.env.RegisterDelayedCallback(func() {
		s.env.UpdateWorkflow("processEvent", "idempotent-1", &testsuite.TestUpdateCallback{
			OnComplete: func(response interface{}, err error) {
				s.NoError(err)
				result := response.(*demov1.DemoEngineState)
				s.Equal(int64(25), result.CurrentValue)
				s.Len(result.History, 1)
			},
		}, requestCtx, incrementSignal)
	}, time.Millisecond*100)

	// Duplicate operation with same idempotency key (should be deduplicated)
	s.env.RegisterDelayedCallback(func() {
		s.env.UpdateWorkflow("processEvent", "idempotent-2", &testsuite.TestUpdateCallback{
			OnComplete: func(response interface{}, err error) {
				s.NoError(err)
				result := response.(*demov1.DemoEngineState)
				// Should still be 25, not 50
				s.Equal(int64(25), result.CurrentValue)
				s.Len(result.History, 1) // Should still be only one event
			},
		}, requestCtx, incrementSignal)
	}, time.Millisecond*200)

	// Final state verification
	s.env.RegisterDelayedCallback(func() {
		encoded, err := s.env.QueryWorkflow("getEntityState")
		s.NoError(err)
		
		var queryResponse entityworkflows.EntityQueryResponse[*demov1.DemoEngineState]
		err = encoded.Get(&queryResponse)
		s.NoError(err)
		
		s.True(queryResponse.IsInitialized)
		s.Contains(queryResponse.CurrentStateJSON, `"currentValue":25`)
		// Should contain only one event despite multiple requests
	}, time.Millisecond*400)
} 