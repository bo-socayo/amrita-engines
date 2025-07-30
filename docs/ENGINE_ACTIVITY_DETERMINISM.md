# Engine Activity Determinism Design

## 🚨 **CRITICAL DEPLOYMENT BLOCKER**

**Status**: MUST FIX BEFORE DEPLOYMENT  
**Priority**: P0 - Blocks Production Release  
**Risk**: Workflow non-determinism will cause replay failures and data corruption

## Problem Statement

Currently, our `EntityWorkflow` calls engine methods directly from workflow code:

```go
// ❌ BROKEN - Non-deterministic workflow code
newState, transitionInfo, err := uh.engine.ProcessEvent(engineCtx, envelope)
```

**This violates Temporal's determinism requirements because:**

1. **Engine Logic Evolution**: Engines will change over time (bug fixes, feature additions)
2. **Replay Failures**: Old workflow histories will replay differently with new engine versions
3. **State Corruption**: Non-deterministic replays can cause data inconsistency
4. **Production Incidents**: Workflow failures in production when engine code changes

## Solution Architecture

### **Core Principle: Activity Isolation**

**All engine interactions MUST happen in Temporal Activities, never in Workflow code.**

```go
// ✅ CORRECT - Deterministic workflow code
activityResult := workflow.ExecuteLocalActivity(ctx, ProcessEventActivity, activityInput)
```

### **Why Local Activities?**

1. **Performance**: No network round-trip (same worker process)
2. **Sequential Processing**: Maintains strict ordering requirements
3. **Blocking Execution**: Engine calls must complete before next event
4. **State Consistency**: Prevents race conditions in state transitions

## Implementation Design

### **1. Activity Definition**

```go
// activities/engine_activities.go
package activities

type ProcessEventActivityInput struct {
    EntityType      string                `json:"entity_type"`
    SequenceNumber  int64                `json:"sequence_number"`
    EventEnvelope   *engines.TypedEventEnvelope[TEvent] `json:"event_envelope"`
    CurrentState    TState               `json:"current_state"`
}

type ProcessEventActivityResult struct {
    NewState        TState               `json:"new_state"`
    TransitionInfo  TTransitionInfo      `json:"transition_info"`
    SequenceNumber  int64                `json:"sequence_number"`
    Success         bool                 `json:"success"`
    ErrorMessage    string               `json:"error_message,omitempty"`
}

// ProcessEventActivity wraps engine.ProcessEvent for deterministic execution
func ProcessEventActivity(ctx context.Context, input ProcessEventActivityInput) (*ProcessEventActivityResult, error) {
    // Get engine instance (from registry/factory)
    engine := GetEngineForType(input.EntityType)
    
    // Initialize engine with current state
    if !engine.IsInitialized() {
        _, err := engine.SetInitialState(ctx, input.CurrentState, timestamppb.Now())
        if err != nil {
            return &ProcessEventActivityResult{
                Success: false,
                ErrorMessage: fmt.Sprintf("engine initialization failed: %v", err),
                SequenceNumber: input.SequenceNumber,
            }, nil // Don't return error - return result with failure flag
        }
    }
    
    // Process event through engine
    newState, transitionInfo, err := engine.ProcessEvent(ctx, input.EventEnvelope)
    if err != nil {
        return &ProcessEventActivityResult{
            Success: false,
            ErrorMessage: fmt.Sprintf("engine processing failed: %v", err),
            SequenceNumber: input.SequenceNumber,
        }, nil
    }
    
    return &ProcessEventActivityResult{
        NewState: newState,
        TransitionInfo: transitionInfo,
        SequenceNumber: input.SequenceNumber,
        Success: true,
    }, nil
}
```

### **2. Updated Workflow Integration**

```go
// entity_workflow.go - Updated ProcessEvent handler
func (ctx workflow.Context, requestCtx *entityv1.RequestContext, event TEvent) (TState, error) {
    // ... authorization and idempotency checks ...
    
    // ✅ Execute engine processing in LOCAL ACTIVITY
    activityInput := ProcessEventActivityInput{
        EntityType:     entityType,
        SequenceNumber: sequenceNumber,
        EventEnvelope:  envelope,
        CurrentState:   currentState,
    }
    
    // Configure local activity options
    localActivityOptions := workflow.LocalActivityOptions{
        ScheduleToCloseTimeout: 30 * time.Second, // Engine processing should be fast
        RetryPolicy: &temporal.RetryPolicy{
            MaximumAttempts: 3, // Limited retries for engine failures
        },
    }
    
    ctx = workflow.WithLocalActivityOptions(ctx, localActivityOptions)
    
    // Execute blocking local activity
    var activityResult ProcessEventActivityResult
    err := workflow.ExecuteLocalActivity(ctx, ProcessEventActivity, activityInput).Get(ctx, &activityResult)
    if err != nil {
        logger.Error("❌ Engine activity execution failed", "error", err)
        return zero, fmt.Errorf("engine activity failed: %w", err)
    }
    
    // Handle engine processing failure
    if !activityResult.Success {
        logger.Error("❌ Engine processing failed", "error", activityResult.ErrorMessage)
        return zero, fmt.Errorf("engine processing failed: %s", activityResult.ErrorMessage)
    }
    
    // ✅ CRITICAL: Verify sequence number consistency
    if activityResult.SequenceNumber != sequenceNumber {
        logger.Error("❌ Sequence number mismatch - potential state corruption",
            "expected", sequenceNumber,
            "received", activityResult.SequenceNumber)
        return zero, fmt.Errorf("sequence number mismatch: expected %d, got %d", 
            sequenceNumber, activityResult.SequenceNumber)
    }
    
    // Update workflow state with engine result
    currentState = activityResult.NewState
    
    // ... rest of workflow logic ...
    
    return currentState, nil
}
```

### **3. Engine Registry/Factory**

```go
// activities/engine_registry.go
package activities

var engineRegistry = make(map[string]func() interface{})

func RegisterEngine[TState, TEvent, TTransitionInfo proto.Message](
    entityType string,
    factory func() engines.Engine[TState, TEvent, TTransitionInfo],
) {
    engineRegistry[entityType] = func() interface{} {
        return factory()
    }
}

func GetEngineForType(entityType string) interface{} {
    factory, exists := engineRegistry[entityType]
    if !exists {
        panic(fmt.Sprintf("no engine registered for entity type: %s", entityType))
    }
    return factory()
}

// Initialize in main.go or init function
func init() {
    RegisterEngine("demo.Entity", func() engines.Engine[*demov1.DemoEntityState, *demov1.DemoEntityEvent, *demov1.DemoTransitionInfo] {
        return demo.NewDemoEngine()
    })
}
```

## State Consistency & Sequence Numbers

### **Critical Invariant: Sequential Processing**

```go
// Workflow state tracking
type WorkflowState struct {
    sequenceNumber    int64  // MUST increment for each event
    lastProcessedSeq  int64  // Last successfully processed sequence
    // ... other fields
}

// Before processing
sequenceNumber := workflowState.IncrementSequenceNumber()

// After successful processing
if activityResult.SequenceNumber == sequenceNumber {
    workflowState.lastProcessedSeq = sequenceNumber
    // Safe to update currentState
} else {
    // ABORT - sequence mismatch indicates corruption
    return temporal.NewApplicationError("sequence-mismatch", "SequenceError")
}
```

### **Handling Activity Failures**

```go
// Retry Policy for Engine Activities
RetryPolicy: &temporal.RetryPolicy{
    InitialInterval: 100 * time.Millisecond,
    BackoffCoefficient: 1.5,
    MaximumInterval: 5 * time.Second,
    MaximumAttempts: 3, // Limited - engine failures should be deterministic
    NonRetryableErrorTypes: []string{
        "ValidationError",      // Business logic errors
        "BusinessRuleViolation", // Should not retry
    },
}
```

## Migration Strategy

### **Phase 1: Activity Infrastructure**
1. ✅ Create `activities/` package
2. ✅ Implement `ProcessEventActivity`
3. ✅ Set up engine registry
4. ✅ Add activity registration to worker

### **Phase 2: Workflow Updates** 
1. ✅ Update `UpdateHandler.ProcessEvent()` to use activity
2. ✅ Add sequence number validation
3. ✅ Update error handling for activity failures
4. ✅ Maintain backward compatibility during transition

### **Phase 3: Testing & Validation**
1. ✅ Unit tests for activities
2. ✅ Integration tests with activity mocking
3. ✅ Replay testing with old/new versions
4. ✅ Performance testing (local activities should be fast)

### **Phase 4: Deployment**
1. ✅ Deploy activity workers first
2. ✅ Deploy updated workflow code
3. ✅ Monitor for determinism issues
4. ✅ Rollback plan if replay failures occur

## Performance Considerations

### **Local Activity Benefits:**
- **No Network Overhead**: Executes in same worker process
- **Fast Execution**: Engine processing typically < 100ms
- **Memory Efficiency**: No serialization for complex state objects
- **Debugging**: Easier to trace compared to regular activities

### **Timeout Configuration:**
```go
LocalActivityOptions{
    ScheduleToCloseTimeout: 30 * time.Second,  // Generous timeout
    StartToCloseTimeout:    30 * time.Second,  // Same as schedule-to-close
}
```

## Monitoring & Observability

### **Key Metrics to Track:**
1. **Activity Execution Time**: Engine processing duration
2. **Activity Failure Rate**: Engine processing failures
3. **Sequence Number Gaps**: Detect state inconsistencies  
4. **Replay Success Rate**: Determinism validation

### **Alerts:**
- **High Activity Failure Rate** → Engine logic issues
- **Sequence Number Mismatches** → Critical state corruption
- **Activity Timeouts** → Performance degradation

## Testing Strategy

### **Unit Tests for Activities:**
```go
func TestProcessEventActivity_Success(t *testing.T) {
    input := ProcessEventActivityInput{
        EntityType: "demo.Entity", 
        SequenceNumber: 123,
        EventEnvelope: testEnvelope,
        CurrentState: testState,
    }
    
    result, err := ProcessEventActivity(context.Background(), input)
    
    assert.NoError(t, err)
    assert.True(t, result.Success)
    assert.Equal(t, int64(123), result.SequenceNumber)
}
```

### **Integration Tests - PREFER REAL LOCAL ACTIVITIES:**

```go
// ✅ PREFERRED - Use real local activities for most tests
func TestWorkflow_WithRealLocalActivity(t *testing.T) {
    env := suite.NewTestWorkflowEnvironment()
    
    // Register REAL activity - no mocking needed!
    env.RegisterActivity(ProcessEventActivity)
    
    // Test with real engine processing
    demoEngine := demo.NewDemoEngine()
    env.ExecuteWorkflow(EntityWorkflow, testParams, demoEngine, idHandler)
    
    assert.True(t, env.IsWorkflowCompleted())
    
    // Verify actual state transitions - tests real engine logic
    var finalState *demov1.DemoEntityState
    env.GetWorkflowResult(&finalState)
    assert.Equal(t, expectedRealState, finalState)
}

// ✅ Mock only for specific error scenarios
func TestWorkflow_ActivityTimeoutHandling(t *testing.T) {
    env := suite.NewTestWorkflowEnvironment()
    
    // Mock ONLY to simulate timeout failures
    env.OnActivity(ProcessEventActivity, mock.Anything).Return(
        nil, temporal.NewTimeoutError(enumspb.TIMEOUT_TYPE_SCHEDULE_TO_CLOSE))
    
    env.ExecuteWorkflow(EntityWorkflow, testParams, engine, idHandler)
    
    // Verify timeout error handling
    assert.True(t, env.IsWorkflowCompleted())
    assert.Error(t, env.GetWorkflowError())
}

func TestWorkflow_SequenceNumberMismatch(t *testing.T) {
    env := suite.NewTestWorkflowEnvironment()
    
    // Mock to simulate sequence number corruption
    env.OnActivity(ProcessEventActivity, mock.Anything).Return(
        &ProcessEventActivityResult{
            Success: true,
            SequenceNumber: 999, // Wrong sequence number
            NewState: someState,
        }, nil)
    
    env.ExecuteWorkflow(EntityWorkflow, testParams, engine, idHandler)
    
    // Verify sequence mismatch detection
    assert.Error(t, env.GetWorkflowError())
    assert.Contains(t, env.GetWorkflowError().Error(), "sequence number mismatch")
}

func TestWorkflow_EngineProcessingFailure(t *testing.T) {
    env := suite.NewTestWorkflowEnvironment()
    
    // Mock to simulate engine processing failures
    env.OnActivity(ProcessEventActivity, mock.Anything).Return(
        &ProcessEventActivityResult{
            Success: false,
            ErrorMessage: "simulated engine failure",
            SequenceNumber: 1,
        }, nil)
    
    env.ExecuteWorkflow(EntityWorkflow, testParams, engine, idHandler)
    
    // Verify engine failure handling
    assert.Error(t, env.GetWorkflowError())
    assert.Contains(t, env.GetWorkflowError().Error(), "engine processing failed")
}
```

### **Why Real Local Activities are Better:**

1. **Fast Execution**: Local activities run in-process, so tests are still fast
2. **Real Engine Logic**: Test actual business logic, not mocked behavior
3. **Full Integration**: Catch real issues between workflow and engine
4. **Deterministic**: Engine logic should be deterministic anyway
5. **Simpler Tests**: No complex mocking setup for happy path
6. **Better Coverage**: Tests reflect actual production behavior

### **When to Mock Local Activities:**

- **Timeout testing**: Simulate activity timeouts
- **Engine failures**: Test specific error conditions
- **Sequence corruption**: Test state consistency validation
- **Performance testing**: Control timing for race condition tests

### **Test Categories:**

```go
// 1. Happy Path - Real Activities
func TestWorkflow_SuccessfulEventProcessing(t *testing.T) {
    // Use real activities, real engines
    // Test actual state transitions
}

// 2. Error Handling - Mocked Failures  
func TestWorkflow_ErrorScenarios(t *testing.T) {
    // Mock specific failures
    // Test error handling logic
}

// 3. Edge Cases - Mixed Approach
func TestWorkflow_EdgeCases(t *testing.T) {
    // Real activities for setup
    // Mocked failures for specific edge cases
}
```

## Risk Mitigation

### **Deployment Risks:**
1. **Replay Failures**: Extensive replay testing required
2. **Performance Regression**: Monitor activity execution times
3. **State Corruption**: Sequence number validation prevents this
4. **Activity Timeouts**: Generous timeouts + monitoring

### **Rollback Plan:**
1. **Immediate**: Disable new workflow versions
2. **Emergency**: Revert to direct engine calls (temporary)
3. **Recovery**: Fix issues and re-deploy with activity approach

## Conclusion

**This activity-based architecture is MANDATORY for production deployment.** It ensures:

✅ **Deterministic Workflows**: Engine changes won't break replays  
✅ **State Consistency**: Sequential processing with sequence validation  
✅ **Performance**: Local activities minimize overhead  
✅ **Reliability**: Proper error handling and retry policies  

**Next Steps:**
1. Implement activity infrastructure
2. Update workflow integration
3. Comprehensive testing
4. Phased deployment with monitoring

**DO NOT DEPLOY without this fix - it will cause production incidents.** 