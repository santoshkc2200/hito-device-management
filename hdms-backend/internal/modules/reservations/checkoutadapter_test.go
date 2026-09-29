package reservations

import (
	"testing"
	"time"
)

func TestPolicyLeadIsTheLongerOfPreWindowAndBufferPlusMinimumLoan(t *testing.T) {
	cases := []struct {
		name string
		p    policy
		want time.Duration
	}{
		{"buffer dominates", policy{preWindow: 30 * time.Minute, buffer: 60 * time.Minute}, 90 * time.Minute},
		{"pre-window dominates", policy{preWindow: 3 * time.Hour, buffer: 60 * time.Minute}, 3 * time.Hour},
		{"zero buffer still keeps the minimum loan", policy{preWindow: 10 * time.Minute, buffer: 0}, 30 * time.Minute},
	}
	for _, tc := range cases {
		if got := tc.p.lead(); got != tc.want {
			t.Errorf("%s: lead() = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestLatestReturn(t *testing.T) {
	from := time.Date(2026, 9, 29, 10, 0, 0, 0, time.UTC)
	p := policy{buffer: 60 * time.Minute, maxLoan: 30 * 24 * time.Hour}

	if got, want := latestReturn(from, nil, p), from.Add(30*24*time.Hour); !got.Equal(want) {
		t.Errorf("no next reservation: latest = %v, want %v", got, want)
	}
	next := time.Date(2026, 9, 30, 10, 0, 0, 0, time.UTC)
	if got, want := latestReturn(from, &next, p), next.Add(-time.Hour); !got.Equal(want) {
		t.Errorf("next reservation tomorrow 10:00: latest = %v, want %v", got, want)
	}
	far := from.Add(60 * 24 * time.Hour)
	if got, want := latestReturn(from, &far, p), from.Add(30*24*time.Hour); !got.Equal(want) {
		t.Errorf("next reservation beyond max: latest = %v, want %v", got, want)
	}
}
