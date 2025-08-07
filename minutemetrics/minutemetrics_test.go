package minutemetrics

import (
	"testing"
)

func TestCalculateCounterIncrease(t *testing.T) {
	tests := []struct {
		name             string
		previous         uint64
		current          uint64
		expectedIncrease uint64
	}{
		{
			name:             "No increase",
			previous:         100,
			current:          100,
			expectedIncrease: 0,
		},
		{
			name:             "Simple increase",
			previous:         100,
			current:          200,
			expectedIncrease: 100,
		},
		{
			name:             "Counter wraparound with threshold",
			previous:         Threshold + 5000,
			current:          3000,
			expectedIncrease: (MaxCounterValue - (Threshold + 5000)) + 3000 + 1,
		},
		{
			name:             "Counter wraparound without threshold",
			previous:         MaxCounterValue - 1000,
			current:          500,
			expectedIncrease: 1501,
		},
		{
			name:             "Wraparound with previous below threshold",
			previous:         10,
			current:          20,
			expectedIncrease: 10,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mm := &MinuteMetricManager{}
			result := mm.CalculateCounterIncrease(tt.previous, tt.current)
			if result != tt.expectedIncrease {
				t.Errorf("CalculateCounterIncrease(%d, %d) = %d; want %d", tt.previous, tt.current, result, tt.expectedIncrease)
			}
		})
	}
}
