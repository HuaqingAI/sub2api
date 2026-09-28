package main

import (
	"testing"
	"time"
)

func TestParseShutdownTimeout(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		raw   string
		want  time.Duration
		valid bool
	}{
		{
			name:  "empty uses default",
			raw:   "",
			want:  defaultShutdownTimeout,
			valid: true,
		},
		{
			name:  "whitespace uses default",
			raw:   "  ",
			want:  defaultShutdownTimeout,
			valid: true,
		},
		{
			name:  "parses seconds",
			raw:   "300s",
			want:  300 * time.Second,
			valid: true,
		},
		{
			name:  "parses compound duration",
			raw:   "1m30s",
			want:  90 * time.Second,
			valid: true,
		},
		{
			name:  "invalid duration uses default",
			raw:   "not-a-duration",
			want:  defaultShutdownTimeout,
			valid: false,
		},
		{
			name:  "zero duration uses default",
			raw:   "0s",
			want:  defaultShutdownTimeout,
			valid: false,
		},
		{
			name:  "negative duration uses default",
			raw:   "-1s",
			want:  defaultShutdownTimeout,
			valid: false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, valid := parseShutdownTimeout(tt.raw)
			if got != tt.want {
				t.Fatalf("parseShutdownTimeout(%q) duration = %s, want %s", tt.raw, got, tt.want)
			}
			if valid != tt.valid {
				t.Fatalf("parseShutdownTimeout(%q) valid = %t, want %t", tt.raw, valid, tt.valid)
			}
		})
	}
}
