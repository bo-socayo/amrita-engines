# Entity Workflow Client Architecture

## 🎯 **Core Problem Statement**

How should clients interact with entity workflows? What should clients provide vs. what should workflows manage internally? How does workflow discovery work?

## 🏗️ **Architecture Overview**

### **Client Responsibilities**
1. **Authorization Context**: Who is making this request?
2. **Request Identity**: Idempotency and tracing
3. **Business Operation**: What operation to perform (via protobuf message)
4. **Workflow Discovery**: Which entity workflow to target

### **Workflow Responsibilities**  
1. **Entity Identity**: Managing its own identity and state
2. **State Management**: Sequence numbers, timestamps, continue-as-new
3. **Operation Processing**: Business logic execution

---

## 📋 **Detailed Breakdown**

### **1. RequestContext (Client-Provided)**

```protobuf
message RequestContext {
  // 🔐 AUTHORIZATION (per-request - eventually from JWT)
  string org_id = 1;           // Organization making the request
  string user_id = 2;          // User making the request  
  string tenant = 3;           // Tenant context
  string environment = 4;      // Environment (dev/staging/prod)
  
  // 🆔 REQUEST IDENTITY & DEDUPLICATION  
  string idempotency_key = 5;  // Client-driven deduplication
  google.protobuf.Timestamp request_time = 6; // When request was made
  
  // 📊 OBSERVABILITY (optional)
  string request_id = 7;       // Request tracing ID
  string trace_id = 8;         // Distributed tracing ID
}
```

**Client creates this fresh for every request.**

### **2. EntityMetadata (Workflow-Managed)**

```protobuf
message EntityMetadata {
  // 🔵 ENTITY IDENTITY (workflow-managed)
  string entity_id = 1;        // The entity this workflow represents
  string entity_type = 2;      // Protobuf type name (auto-derived)
  
  // 🟢 AUTHORIZATION & TENANCY CONTEXT (set once at workflow start)
  string org_id = 3;           // Organization identifier
  string user_id = 5;          // Acting user
  string environment = 6;      // Environment
  string tenant = 7;           // Top-level tenant isolation
  
  // 🟡 WORKFLOW METADATA (workflow-managed)
  google.protobuf.Timestamp created_at = 8; // When workflow started
}
```

**Workflow manages this internally. Client never touches it.**

### **3. Internal Workflow State (Never Exposed)**

```go
// ✅ Internal to workflow - client never sees this
var sequenceNumber int64 = 0  // Monotonic counter for event envelopes

// Used only for event envelope creation:
envelope, err := engines.NewTypedEventEnvelope(
    sequenceNumber,  // ← Pure workflow internal state
    eventID,
    timestamp,
    event,
    nil,
)
```

**Completely hidden from clients. Used only for event sourcing consistency.**

### **4. Business Operation (Protobuf Message)**

```protobuf
// Client sends the actual business operation
message AddUserMessageSignal {
  UserMessage message = 1;
  Timestamp timestamp = 2;
}
```

**The protobuf message type IS the operation type. No separate "signal_type" field needed.**

---

## 🎯 **Workflow Discovery: How Clients Find Workflows**

### **Current Pattern**
```go
// ❓ How does client know to call this specific workflow?
workflowID := "conversation-123"  // ← How is this derived?
taskQueue := "llm-conversation-workers"  // ← How does client know this?
```

### **Proposed Pattern: Engine-Based Discovery**

```go
// 1. Client knows the ENTITY they want to interact with
entityID := "conversation-123" 
entityType := "LLMConversationHistory"  // From protobuf

// 2. Client routes to the appropriate ENGINE
engineName := "llm-conversation-history"  // Maps to entityType
taskQueue := engineName + "-workers"     // Convention: {engine}-workers
workflowID := entityType + "-" + entityID // Convention: {type}-{id}

// 3. Client calls the engine's task queue
client.UpdateWorkflow(ctx, client.UpdateWorkflowOptions{
    WorkflowID: workflowID,    // "LLMConversationHistory-conversation-123"
    TaskQueue:  taskQueue,     // "llm-conversation-history-workers"  
    UpdateName: "processEvent",
    Args:       []interface{}{requestCtx, businessMessage},
})
```

### **Engine Registration Pattern**
```go
// Each engine registers itself with a discovery service
type EntityEngine interface {
    GetEntityType() string      // "LLMConversationHistory"
    GetEngineName() string      // "llm-conversation-history"  
    GetTaskQueue() string       // "llm-conversation-history-workers"
}

// Client uses discovery to route requests
engineRegistry.RouteToEngine(entityType, entityID, requestCtx, operation)
```

---

## 🔄 **Complete Flow Example**

### **1. Client Code**
```go
// ✅ Client focuses ONLY on request context + business operation
requestCtx := &entityv1.RequestContext{
    OrgId:          "org-456",
    UserId:         "user-789", 
    Tenant:         "tenant-abc",
    Environment:    "prod",
    IdempotencyKey: "add-msg-" + uuid.New().String()[:8],
    RequestTime:    timestamppb.Now(),
    RequestId:      uuid.New().String(),
}

businessOperation := &llmv1.AddUserMessageSignal{
    Message: &llmv1.UserMessage{Content: "Hello!"},
    Timestamp: timestamppb.Now(),
}

// Client discovers workflow via entity identity
entityID := "conversation-123"
workflowID := "LLMConversationHistory-" + entityID
taskQueue := "llm-conversation-history-workers"

// Send to entity workflow
result, err := client.UpdateWorkflow(ctx, client.UpdateWorkflowOptions{
    WorkflowID: workflowID,
    TaskQueue:  taskQueue,
    UpdateName: "processEvent",
    Args:       []interface{}{requestCtx, businessOperation},
})
```

### **2. Workflow Code**
```go
// ✅ Workflow manages its own EntityMetadata internally
func EntityWorkflow[TState, TEvent, TTransitionInfo proto.Message](
    ctx workflow.Context,
    params EntityWorkflowParams[TState],  // Contains EntityMetadata
    engine engines.Engine[TState, TEvent, TTransitionInfo],
    idHandler WorkflowIDHandler,
) error {
    // Workflow owns its EntityMetadata
    entityMetadata := params.Metadata  // Set once at workflow start
    
    workflow.SetUpdateHandler(ctx, "processEvent", 
        func(ctx workflow.Context, requestCtx *entityv1.RequestContext, event TEvent) (TState, error) {
            // ✅ Verify authorization using RequestContext
            if !authorizeRequest(requestCtx, entityMetadata) {
                return zero, fmt.Errorf("unauthorized")
            }
            
            // ✅ Check idempotency using RequestContext
            if isAlreadyProcessed(requestCtx.IdempotencyKey, requestCtx.RequestTime) {
                return currentState, nil
            }
            
            // ✅ Process business operation
            // entityMetadata.SequenceNumber++ (workflow manages this)
            newState, err := engine.ProcessEvent(ctx, event)
            
            return newState, err
        })
}

func authorizeRequest(requestCtx *entityv1.RequestContext, entityMetadata *entityv1.EntityMetadata) bool {
    // Verify requestCtx.OrgId has access to this entity
    // Verify requestCtx.UserId has permission for this operation
    // etc.
    return true
}
```

---

## 🛤️ **Migration Path to JWT**

### **Current State**
```go
// Client manually creates RequestContext
requestCtx := &entityv1.RequestContext{
    OrgId:   "org-456",  // ← Client manually sets
    UserId:  "user-789", // ← Client manually sets  
    // ...
}
```

### **Future State with JWT**
```go
// 1. Client provides JWT token
jwtToken := "eyJ0eXAiOiJKV1QiLCJhbGciOiJIUzI1NiJ9..."

// 2. Temporal interceptor automatically extracts claims
// (No RequestContext creation needed by client)

// 3. Workflow receives pre-populated RequestContext
func(ctx workflow.Context, requestCtx *entityv1.RequestContext, event TEvent) {
    // requestCtx automatically populated from JWT:
    // requestCtx.OrgId   ← from JWT "org" claim
    // requestCtx.UserId  ← from JWT "sub" claim  
    // requestCtx.Tenant  ← from JWT "tenant" claim
}
```

---

## 📝 **Key Decisions & Rationale**

### **✅ What We Decided**

1. **RequestContext = Authorization + Request Identity**
   - Client provides: org_id, user_id, tenant, environment, idempotency_key
   - Workflow verifies authorization and handles deduplication

2. **EntityMetadata = Workflow Identity + State**  
   - Workflow manages: entity_id, entity_type, sequence_number
   - Client never touches internal workflow state

3. **Protobuf Message = Operation Type**
   - No separate "signal_type" field needed
   - The protobuf message type IS the operation

4. **Engine-Based Workflow Discovery**
   - Convention: workflowID = entityType + "-" + entityID  
   - Convention: taskQueue = engineName + "-workers"
   - Engine registry for routing

### **🎯 Benefits**

- **Clear Separation**: Client = request context, Workflow = entity state
- **Security**: Authorization verified on every request
- **Idempotency**: Request-level deduplication with time-based cutoffs  
- **Scalability**: Engine-based routing supports multiple entity types
- **Future-Proof**: Clear migration path to JWT/interceptors

### **🔄 Next Steps**

1. ✅ Update RequestContext protobuf with authorization fields
2. ✅ Remove authorization fields from EntityMetadata  
3. ✅ Delete unnecessary client helper abstractions
4. ✅ Update example clients to use new RequestContext
5. 🔄 Implement engine discovery registry
6. 🔄 Add authorization verification in workflows
7. 🔄 Design JWT interceptor architecture
