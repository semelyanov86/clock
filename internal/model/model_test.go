package model

import "testing"

func TestDeltaDirection(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		delta Delta
		want  Direction
	}{
		{"rise", Delta{Abs: 1.2, Pct: 0.8}, Up},
		{"fall", Delta{Abs: -1.2, Pct: -0.8}, Down},
		{"unchanged", Delta{}, Flat},
		{
			// XEON.EU on a closed session: ltp 149.918 against a close of 149.919.
			// It prints as −0.00%, so it must not be painted as a fall.
			name:  "a move that rounds to zero is flat",
			delta: Delta{Abs: -0.001, Pct: -0.00067},
			want:  Flat,
		},
		{"just past the rounding threshold", Delta{Pct: -0.006}, Down},
		{
			// Legacy quote shape: no percentage reported, only the absolute.
			name:  "absolute only",
			delta: Delta{Abs: 6.98},
			want:  Up,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.delta.Direction(); got != tt.want {
				t.Errorf("Direction() = %v, want %v", got, tt.want)
			}
		})
	}
}
