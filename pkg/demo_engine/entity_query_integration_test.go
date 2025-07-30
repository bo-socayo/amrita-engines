package demo_engine

import (
	"fmt"
	"testing"
	"time"

	demov1 "github.com/bo-socayo/amrita-engines/gen/engines/demo/v1"
	entityv1 "github.com/bo-socayo/amrita-engines/gen/entity/v1"
	"github.com/bo-socayo/amrita-engines/pkg/entityworkflows"
	"github.com/stretchr/testify/suite"
	"go.temporal.io/sdk/testsuite"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// DemoEntityQueryIntegrationTestSuite tests query handlers in the entity workflow
type DemoEntityQueryIntegrationTestSuite struct {
	suite.Suite
	testsuite.WorkflowTestSuite
	env *testsuite.TestWorkflowEnvironment
}

func (s *DemoEntityQueryIntegrationTestSuite) SetupTest() {
	s.env = s.NewTestWorkflowEnvironment()
}

func (s *DemoEntityQueryIntegrationTestSuite) TearDownTest() {
	s.env.AssertExpectations(s.T())
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

	s.env.ExecuteWorkflow(DemoEntityWorkflow, params)

	// Query before any updates
	encoded, err := s.env.QueryWorkflow("getEntityState")
	s.NoError(err)

	var response entityworkflows.EntityQueryResponse[*demov1.DemoEngineState]
	err = encoded.Get(&response)
	s.NoError(err)

	s.True(response.IsInitialized)
	s.NotEmpty(response.CurrentStateJSON)
	// Metadata may be nil before any operations
	
	// Verify initial state values (currentValue may be omitted when 0 in protobuf JSON)
	s.Contains(response.CurrentStateJSON, `"nextEventId":"1"`)
	s.Contains(response.CurrentStateJSON, `"config"`)
	s.Contains(response.CurrentStateJSON, `"defaultIncrement":"1"`)
}

func (s *DemoEntityQueryIntegrationTestSuite) TestQueryAfterUpdates() {
	params := entityworkflows.EntityWorkflowParams[*demov1.DemoEngineState]{
		EntityId:     "test-query-after-updates",
		InitialState: ApplyBusinessDefaults(nil),
	}

	s.env.ExecuteWorkflow(DemoEntityWorkflow, params)

	// Send update and then query state
	s.env.RegisterDelayedCallback(func() {
		requestCtx := &entityv1.RequestContext{
			UserId:         "test-user",
			OrgId:          "test-org",
			IdempotencyKey: "test-query-1",
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
		s.env.UpdateWorkflow("processEvent", "update-1", &testsuite.TestUpdateCallback{
			OnAccept: func() {
				// Update accepted
			},
			OnReject: func(err error) {
				s.Fail("Update should not be rejected", err)
			},
			OnComplete: func(response interface{}, err error) {
				s.NoError(err)
				
				// Query state after update completes
				encoded, err := s.env.QueryWorkflow("getEntityState")
				s.NoError(err)

				var queryResponse entityworkflows.EntityQueryResponse[*demov1.DemoEngineState]
				err = encoded.Get(&queryResponse)
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
			},
		}, requestCtx, incrementSignal)
	}, time.Millisecond*100)
}

func (s *DemoEntityQueryIntegrationTestSuite) TestQueryAfterMultipleOperations() {
	params := entityworkflows.EntityWorkflowParams[*demov1.DemoEngineState]{
		EntityId:     "test-query-multi-ops",
		InitialState: ApplyBusinessDefaults(nil),
	}

	s.env.ExecuteWorkflow(DemoEntityWorkflow, params)

	// Send multiple operations with proper timing
	s.env.RegisterDelayedCallback(func() {
		// First increment
		requestCtx1 := &entityv1.RequestContext{
			UserId:         "test-user",
			OrgId:          "test-org",
			IdempotencyKey: "inc-1",
		}

		incrementSignal1 := &demov1.DemoEngineSignal{
			Signal: &demov1.DemoEngineSignal_Increment{
				Increment: &demov1.IncrementSignal{Amount: 3, Reason: "First increment", Timestamp: timestamppb.Now()},
			},
		}

		s.env.UpdateWorkflow("processEvent", "inc-1", &testsuite.TestUpdateCallback{
			OnComplete: func(response interface{}, err error) {
				s.NoError(err)
			},
		}, requestCtx1, incrementSignal1)
	}, time.Millisecond*100)

	s.env.RegisterDelayedCallback(func() {
		// Second increment
		requestCtx2 := &entityv1.RequestContext{
			UserId:         "test-user",
			OrgId:          "test-org",
			IdempotencyKey: "inc-2",
		}

		incrementSignal2 := &demov1.DemoEngineSignal{
			Signal: &demov1.DemoEngineSignal_Increment{
				Increment: &demov1.IncrementSignal{Amount: 7, Reason: "Second increment", Timestamp: timestamppb.Now()},
			},
		}

		s.env.UpdateWorkflow("processEvent", "inc-2", &testsuite.TestUpdateCallback{
			OnComplete: func(response interface{}, err error) {
				s.NoError(err)
			},
		}, requestCtx2, incrementSignal2)
	}, time.Millisecond*200)

	s.env.RegisterDelayedCallback(func() {
		// Reset
		requestCtx3 := &entityv1.RequestContext{
			UserId:         "test-user",
			OrgId:          "test-org",
			IdempotencyKey: "reset-1",
		}

		resetSignal := &demov1.DemoEngineSignal{
			Signal: &demov1.DemoEngineSignal_Reset_{
				Reset_: &demov1.ResetSignal{Reason: "Test reset", Timestamp: timestamppb.Now()},
			},
		}

		s.env.UpdateWorkflow("processEvent", "reset-1", &testsuite.TestUpdateCallback{
			OnComplete: func(response interface{}, err error) {
				s.NoError(err)
				
				// Final query to verify complete history after all operations
				encoded, err := s.env.QueryWorkflow("getEntityState")
				s.NoError(err)

				var finalResponse entityworkflows.EntityQueryResponse[*demov1.DemoEngineState]
				err = encoded.Get(&finalResponse)
				s.NoError(err)

				// Should have 3 events in history and current value should be 0 (after reset)
				s.Contains(finalResponse.CurrentStateJSON, `"currentValue":"0"`)
				s.Contains(finalResponse.CurrentStateJSON, `"nextEventId":"4"`)
			},
		}, requestCtx3, resetSignal)
	}, time.Millisecond*300)
}

func (s *DemoEntityQueryIntegrationTestSuite) TestQueryPerformanceWithLargeHistory() {
	params := entityworkflows.EntityWorkflowParams[*demov1.DemoEngineState]{
		EntityId:     "test-query-large-history",
		InitialState: ApplyBusinessDefaults(nil),
	}

	s.env.ExecuteWorkflow(DemoEntityWorkflow, params)

	// Create large history with proper timing (reduced for test performance)
	numOperations := 10
	
	// Send updates with timing coordination
	for i := 0; i < numOperations; i++ {
		func(index int) {
			s.env.RegisterDelayedCallback(func() {
				requestCtx := &entityv1.RequestContext{
					UserId:         "test-user",
					OrgId:          "test-org",
					IdempotencyKey: fmt.Sprintf("bulk-%d", index),
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

				callback := &testsuite.TestUpdateCallback{
					OnComplete: func(response interface{}, err error) {
						s.NoError(err)
					},
				}

				// On the last operation, also run the performance query
				if index == numOperations-1 {
					callback.OnComplete = func(response interface{}, err error) {
						s.NoError(err)
						
						// Query with large state - measure performance
						start := time.Now()
						encoded, err := s.env.QueryWorkflow("getEntityState")
						queryDuration := time.Since(start)

						s.NoError(err)
						s.Less(queryDuration, 5*time.Second, "Query should complete within 5 seconds even with large history")

						var queryResponse entityworkflows.EntityQueryResponse[*demov1.DemoEngineState]
						err = encoded.Get(&queryResponse)
						s.NoError(err)

						// Verify final state
						expectedValue := int64(numOperations)
						s.Contains(queryResponse.CurrentStateJSON, `"currentValue":"`+fmt.Sprintf("%d", expectedValue)+`"`)
						s.True(len(queryResponse.CurrentStateJSON) > 200, "JSON should be substantial with history")
					}
				}

				s.env.UpdateWorkflow("processEvent", fmt.Sprintf("bulk-%d", index), callback, requestCtx, incrementSignal)
			}, time.Duration(100+index*10)*time.Millisecond)
		}(i)
	}
}

func (s *DemoEntityQueryIntegrationTestSuite) TestQueryWithIdempotentOperations() {
	params := entityworkflows.EntityWorkflowParams[*demov1.DemoEngineState]{
		EntityId:     "test-query-idempotent",
		InitialState: ApplyBusinessDefaults(nil),
	}

	s.env.ExecuteWorkflow(DemoEntityWorkflow, params)

	// Send same operation twice with proper timing
	s.env.RegisterDelayedCallback(func() {
		requestCtx := &entityv1.RequestContext{
			UserId:         "test-user",
			OrgId:          "test-org",
			IdempotencyKey: "duplicate-test",
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
		s.env.UpdateWorkflow("processEvent", "duplicate-1", &testsuite.TestUpdateCallback{
			OnComplete: func(response interface{}, err error) {
				s.NoError(err)
			},
		}, requestCtx, incrementSignal)
	}, time.Millisecond*100)

	s.env.RegisterDelayedCallback(func() {
		requestCtx := &entityv1.RequestContext{
			UserId:         "test-user",
			OrgId:          "test-org",
			IdempotencyKey: "duplicate-test", // Same idempotency key
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

		// Second update with same idempotency key (should be deduplicated)
		s.env.UpdateWorkflow("processEvent", "duplicate-2", &testsuite.TestUpdateCallback{
			OnComplete: func(response interface{}, err error) {
				s.NoError(err)
				
				// Query final state after both operations
				encoded, err := s.env.QueryWorkflow("getEntityState")
				s.NoError(err)

				var queryResponse entityworkflows.EntityQueryResponse[*demov1.DemoEngineState]
				err = encoded.Get(&queryResponse)
				s.NoError(err)

				// Should only have processed once due to idempotency
				s.Contains(queryResponse.CurrentStateJSON, `"currentValue":"10"`)
				s.Contains(queryResponse.CurrentStateJSON, `"nextEventId":"2"`) // Only one event processed
			},
		}, requestCtx, incrementSignal)
	}, time.Millisecond*200)
}

// Helper function to create demo signals
func mustMarshalDemoSignal(signal *demov1.DemoEngineSignal) *anypb.Any {
	data, err := anypb.New(signal)
	if err != nil {
		panic(err)
	}
	return data
} 