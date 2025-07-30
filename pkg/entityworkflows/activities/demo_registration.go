package activities

import (
	"github.com/bo-socayo/amrita-engines/pkg/engines"
	demov1 "github.com/bo-socayo/amrita-engines/gen/engines/demo/v1"
)

// RegisterDemoEngine registers the demo engine with the activity registry
// This is an example of how to register engines for activity execution
func RegisterDemoEngine() {
	// Register the demo engine factory
	RegisterEngine[*demov1.DemoEngineState, *demov1.DemoEngineSignal, *demov1.DemoEngineTransitionInfo](
		"demo.Engine", // Entity type derived from protobuf
		func() engines.Engine[*demov1.DemoEngineState, *demov1.DemoEngineSignal, *demov1.DemoEngineTransitionInfo] {
			// Create demo engine with the same configuration as used in the workflow
			config := engines.BaseEngineConfig[*demov1.DemoEngineState, *demov1.DemoEngineSignal, *demov1.DemoEngineTransitionInfo]{
				EngineName:           "demo-engine",
				BusinessLogicVersion: "1.0.0",
				StateTypeName:        "DemoEngineState",
				EventTypeName:        "DemoEngineSignal", 
				TransitionTypeName:   "DemoEngineTransitionInfo",
				// Note: These processor functions would need to be imported from demo package
				// For now, this is a placeholder - in real implementation, you'd import these
				// from the demo engine package or define them here
			}
			return engines.NewBaseEngine(config)
		},
	)
}