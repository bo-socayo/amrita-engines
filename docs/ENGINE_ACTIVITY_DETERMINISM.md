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

### **Unit Tests:**
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

### **Integration Tests:**
```go
func TestWorkflow_WithEngineActivity(t *testing.T) {
    // Test full workflow with mocked engine activity
    env := suite.NewTestWorkflowEnvironment()
    
    // Mock the activity
    env.OnActivity(ProcessEventActivity, mock.Anything).Return(&ProcessEventActivityResult{
        Success: true,
        SequenceNumber: 1,
        NewState: expectedState,
    }, nil)
    
    // Test workflow execution
    env.ExecuteWorkflow(EntityWorkflow, params, engine, idHandler)
    
    assert.True(t, env.IsWorkflowCompleted())
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