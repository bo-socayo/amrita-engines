package demo_engine

import (
	"context"
	"testing"

	basev1 "github.com/bo-socayo/amrita-engines/gen/engines/base/v1"
	demov1 "github.com/bo-socayo/amrita-engines/gen/engines/demo/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestProcessSignal_UnitTests(t *testing.T) {
	t.Parallel()

	t.Run("UnmarshalError", func(t *testing.T) {
		t.Parallel()

		// Create invalid envelope with bad data
		invalidData, err := anypb.New(&demov1.DemoEngineState{}) // Wrong type
		require.NoError(t, err)

		envelope := &basev1.EventEnvelope{
			Data: invalidData,
		}

		state := &demov1.DemoEngineState{}
		_, _, err = ProcessSignal(context.Background(), envelope, state)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to unmarshal demo signal")
	})

	t.Run("UnknownSignalType", func(t *testing.T) {
		t.Parallel()

		// Create signal with nil oneof (should trigger unknown type)
		signal := &demov1.DemoEngineSignal{} // No signal field set
		data, err := anypb.New(signal)
		require.NoError(t, err)

		envelope := &basev1.EventEnvelope{
			Data:      data,
			Timestamp: timestamppb.Now(),
		}

		state := ApplyBusinessDefaults(nil)
		_, _, err = ProcessSignal(context.Background(), envelope, state)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "unknown signal type")
	})

	t.Run("IncrementValidation_ZeroAmount", func(t *testing.T) {
		t.Parallel()

		signal := &demov1.DemoEngineSignal{
			Signal: &demov1.DemoEngineSignal_Increment{
				Increment: &demov1.IncrementSignal{
					Amount: 0, // Invalid: zero amount
				},
			},
		}
		data, err := anypb.New(signal)
		require.NoError(t, err)

		envelope := &basev1.EventEnvelope{
			Data:      data,
			Timestamp: timestamppb.Now(),
		}

		state := ApplyBusinessDefaults(nil)
		_, transitionInfo, err := ProcessSignal(context.Background(), envelope, state)
		
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid increment amount")
		assert.Nil(t, transitionInfo) // transitionInfo is nil on error in current implementation
	})

	t.Run("IncrementValidation_NegativeAmount", func(t *testing.T) {
		t.Parallel()

		signal := &demov1.DemoEngineSignal{
			Signal: &demov1.DemoEngineSignal_Increment{
				Increment: &demov1.IncrementSignal{
					Amount: -5, // Invalid: negative amount
				},
			},
		}
		data, err := anypb.New(signal)
		require.NoError(t, err)

		envelope := &basev1.EventEnvelope{
			Data:      data,
			Timestamp: timestamppb.Now(),
		}

		state := ApplyBusinessDefaults(nil)
		_, transitionInfo, err := ProcessSignal(context.Background(), envelope, state)
		
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid increment amount")
		assert.Nil(t, transitionInfo) // transitionInfo is nil on error in current implementation
	})

	t.Run("IncrementValidation_ExceedsMaxValue", func(t *testing.T) {
		t.Parallel()

		state := ApplyBusinessDefaults(nil)
		state.Config.MaxValue = 10
		state.CurrentValue = 8

		signal := &demov1.DemoEngineSignal{
			Signal: &demov1.DemoEngineSignal_Increment{
				Increment: &demov1.IncrementSignal{
					Amount: 5, // Would make total 13, exceeding max of 10
				},
			},
		}
		data, err := anypb.New(signal)
		require.NoError(t, err)

		envelope := &basev1.EventEnvelope{
			Data:      data,
			Timestamp: timestamppb.Now(),
		}

		_, transitionInfo, err := ProcessSignal(context.Background(), envelope, state)
		
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "increment would exceed max value")
		assert.Nil(t, transitionInfo) // transitionInfo is nil on error in current implementation
	})

	t.Run("IncrementValidation_AllowNegativeDisabled", func(t *testing.T) {
		t.Parallel()

		state := ApplyBusinessDefaults(nil)
		state.Config.AllowNegative = false
		state.CurrentValue = 2

		signal := &demov1.DemoEngineSignal{
			Signal: &demov1.DemoEngineSignal_Increment{
				Increment: &demov1.IncrementSignal{
					Amount: -5, // Would make total -3, but negative not allowed
				},
			},
		}
		data, err := anypb.New(signal)
		require.NoError(t, err)

		envelope := &basev1.EventEnvelope{
			Data:      data,
			Timestamp: timestamppb.Now(),
		}

		_, transitionInfo, err := ProcessSignal(context.Background(), envelope, state)
		
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid increment amount")
		assert.Nil(t, transitionInfo) // transitionInfo is nil on error in current implementation
	})

	t.Run("ResetValidation_NegativeNotAllowed", func(t *testing.T) {
		t.Parallel()

		state := ApplyBusinessDefaults(nil)
		state.Config.AllowNegative = false
		state.CurrentValue = 5

		signal := &demov1.DemoEngineSignal{
			Signal: &demov1.DemoEngineSignal_Reset_{
				Reset_: &demov1.ResetSignal{
					Reason: "Test reset",
				},
			},
		}
		data, err := anypb.New(signal)
		require.NoError(t, err)

		envelope := &basev1.EventEnvelope{
			Data:      data,
			Timestamp: timestamppb.Now(),
		}

		// This should actually work since reset goes to 0, not negative
		newState, transitionInfo, err := ProcessSignal(context.Background(), envelope, state)
		
		assert.NoError(t, err)
		assert.True(t, transitionInfo.ValidationPassed)
		assert.Equal(t, int64(0), newState.CurrentValue)
	})

	t.Run("ConfigValidation_InvalidDefaultIncrement", func(t *testing.T) {
		t.Parallel()

		signal := &demov1.DemoEngineSignal{
			Signal: &demov1.DemoEngineSignal_UpdateConfig{
				UpdateConfig: &demov1.UpdateConfigSignal{
					Config: &demov1.DemoConfig{
						DefaultIncrement: 0, // Invalid: must be positive
						AllowNegative:    true,
					},
				},
			},
		}
		data, err := anypb.New(signal)
		require.NoError(t, err)

		envelope := &basev1.EventEnvelope{
			Data:      data,
			Timestamp: timestamppb.Now(),
		}

		state := ApplyBusinessDefaults(nil)
		_, transitionInfo, err := ProcessSignal(context.Background(), envelope, state)
		
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "default increment must be positive")
		assert.Nil(t, transitionInfo) // transitionInfo is nil on error in current implementation
	})

	t.Run("ConfigValidation_MaxValueExceedsCurrentValue", func(t *testing.T) {
		t.Parallel()

		state := ApplyBusinessDefaults(nil)
		state.CurrentValue = 15 // Current value higher than new max

		signal := &demov1.DemoEngineSignal{
			Signal: &demov1.DemoEngineSignal_UpdateConfig{
				UpdateConfig: &demov1.UpdateConfigSignal{
					Config: &demov1.DemoConfig{
						MaxValue:         10, // Less than current value
						DefaultIncrement: 1,
						AllowNegative:    true,
					},
				},
			},
		}
		data, err := anypb.New(signal)
		require.NoError(t, err)

		envelope := &basev1.EventEnvelope{
			Data:      data,
			Timestamp: timestamppb.Now(),
		}

		_, transitionInfo, err := ProcessSignal(context.Background(), envelope, state)
		
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "current value")
		assert.Contains(t, err.Error(), "exceeds new max value")
		assert.Nil(t, transitionInfo) // transitionInfo is nil on error in current implementation
	})

	t.Run("ConfigValidation_DisallowNegativeWithNegativeValue", func(t *testing.T) {
		t.Parallel()

		state := ApplyBusinessDefaults(nil)
		state.CurrentValue = -5 // Negative current value

		signal := &demov1.DemoEngineSignal{
			Signal: &demov1.DemoEngineSignal_UpdateConfig{
				UpdateConfig: &demov1.UpdateConfigSignal{
					Config: &demov1.DemoConfig{
						DefaultIncrement: 1,
						AllowNegative:    false, // Disallowing negative but current is negative
					},
				},
			},
		}
		data, err := anypb.New(signal)
		require.NoError(t, err)

		envelope := &basev1.EventEnvelope{
			Data:      data,
			Timestamp: timestamppb.Now(),
		}

		_, transitionInfo, err := ProcessSignal(context.Background(), envelope, state)
		
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "is negative but new config disallows negative values")
		assert.Nil(t, transitionInfo) // transitionInfo is nil on error in current implementation
	})
}

func TestApplyBusinessDefaults_UnitTests(t *testing.T) {
	t.Parallel()

	t.Run("NilState", func(t *testing.T) {
		t.Parallel()

		result := ApplyBusinessDefaults(nil)
		
		assert.NotNil(t, result)
		assert.Equal(t, int64(0), result.CurrentValue)
		assert.Equal(t, int64(1), result.NextEventId)
		assert.NotNil(t, result.Config)
		assert.Equal(t, int64(1), result.Config.DefaultIncrement)
		assert.True(t, result.Config.AllowNegative)
		assert.Equal(t, int64(0), result.Config.MaxValue)
		assert.NotNil(t, result.History)
		assert.Len(t, result.History, 0)
		assert.NotNil(t, result.Metadata)
	})

	t.Run("ExistingStateWithoutConfig", func(t *testing.T) {
		t.Parallel()

		state := &demov1.DemoEngineState{
			CurrentValue: 10,
			NextEventId:  5,
		}

		result := ApplyBusinessDefaults(state)
		
		assert.Equal(t, int64(10), result.CurrentValue) // Preserved
		assert.Equal(t, int64(5), result.NextEventId)   // Preserved
		assert.NotNil(t, result.Config)                 // Added
		assert.Equal(t, int64(1), result.Config.DefaultIncrement)
	})

	t.Run("StateWithExistingConfig", func(t *testing.T) {
		t.Parallel()

		existingConfig := &demov1.DemoConfig{
			MaxValue:         100,
			DefaultIncrement: 5,
			AllowNegative:    false,
			Description:      "Custom config",
		}

		state := &demov1.DemoEngineState{
			CurrentValue: 10,
			Config:       existingConfig,
		}

		result := ApplyBusinessDefaults(state)
		
		// Config should be preserved
		assert.Equal(t, existingConfig, result.Config)
		assert.Equal(t, int64(100), result.Config.MaxValue)
		assert.Equal(t, int64(5), result.Config.DefaultIncrement)
		assert.False(t, result.Config.AllowNegative)
	})
} 