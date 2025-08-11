package web

import (
	"testing"
	"time"
)

// TestFormatDuration tests the FormatDuration function
func TestFormatDuration(t *testing.T) {
	tests := []struct {
		name     string
		input    time.Duration
		expected string
	}{
		{
			name:     "Zero duration",
			input:    0,
			expected: "0d 00h 00m",
		},
		{
			name:     "Less than an hour",
			input:    45 * time.Minute,
			expected: "0d 00h 45m",
		},
		{
			name:     "Less than a day",
			input:    23*time.Hour + 59*time.Minute,
			expected: "0d 23h 59m",
		},
		{
			name:     "Exactly one day",
			input:    24 * time.Hour,
			expected: "1d 00h 00m",
		},
		{
			name:     "More than one day",
			input:    53*time.Hour + 45*time.Minute,
			expected: "2d 05h 45m",
		},
		{
			name:     "Multiple days, hours, and minutes",
			input:    123*time.Hour + 59*time.Minute,
			expected: "5d 03h 59m",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatDuration(tt.input)
			if result != tt.expected {
				t.Errorf("FormatDuration(%v) = %s; expected %s", tt.input, result, tt.expected)
			}
		})
	}
}
