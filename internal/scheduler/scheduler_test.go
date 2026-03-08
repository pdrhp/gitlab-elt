package scheduler

import (
	"testing"
	"time"
)

func TestIsPeakHour(t *testing.T) {
	tests := []struct {
		hour int
		want bool
	}{
		{7, false},
		{8, true},
		{12, true},
		{19, true},
		{20, false},
		{0, false},
		{23, false},
	}

	for _, tt := range tests {
		t.Run(time.Date(2025, 1, 1, tt.hour, 0, 0, 0, time.UTC).Format("15:04"), func(t *testing.T) {
			got := isPeakHour(tt.hour)
			if got != tt.want {
				t.Errorf("isPeakHour(%d) = %v, want %v", tt.hour, got, tt.want)
			}
		})
	}
}
