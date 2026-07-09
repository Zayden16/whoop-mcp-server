package main

import "testing"

func TestParseRange(t *testing.T) {
	tests := []struct {
		name      string
		startDate string
		endDate   string
		wantStart string
		wantEnd   string
		wantErr   bool
	}{
		{
			name:      "both dates",
			startDate: "2026-07-01",
			endDate:   "2026-07-07",
			wantStart: "2026-07-01T00:00:00Z",
			wantEnd:   "2026-07-08T00:00:00Z", // inclusive end maps to start of next day
		},
		{
			name:      "start only",
			startDate: "2026-07-01",
			wantStart: "2026-07-01T00:00:00Z",
			wantEnd:   "",
		},
		{
			name:    "end only",
			endDate: "2026-07-07",
			wantEnd: "2026-07-08T00:00:00Z",
		},
		{
			name: "empty",
		},
		{
			name:      "invalid start",
			startDate: "07/01/2026",
			wantErr:   true,
		},
		{
			name:    "invalid end",
			endDate: "not-a-date",
			wantErr: true,
		},
		{
			name:      "month rollover",
			startDate: "2026-01-31",
			endDate:   "2026-01-31",
			wantStart: "2026-01-31T00:00:00Z",
			wantEnd:   "2026-02-01T00:00:00Z",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			start, end, err := parseRange(tt.startDate, tt.endDate)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got start=%q end=%q", start, end)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if start != tt.wantStart {
				t.Errorf("start = %q, want %q", start, tt.wantStart)
			}
			if end != tt.wantEnd {
				t.Errorf("end = %q, want %q", end, tt.wantEnd)
			}
		})
	}
}
