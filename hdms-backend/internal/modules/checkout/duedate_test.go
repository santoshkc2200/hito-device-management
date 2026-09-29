package checkout

import (
	"testing"
	"time"
)

func TestChooseDueAt(t *testing.T) {
	now := time.Date(2026, 9, 29, 10, 0, 0, 0, time.UTC)
	at := func(h int) *time.Time { v := now.Add(time.Duration(h) * time.Hour); return &v }
	latest := now.Add(20 * time.Hour)

	cases := []struct {
		name      string
		w         ReturnWindow
		preferred *time.Time
		category  *time.Time
		want      time.Time
	}{
		{"collected reservation end wins", ReturnWindow{Latest: latest, CollectedEndAt: *at(6)}, at(3), at(48), *at(6)},
		{"preferred beats category", ReturnWindow{Latest: latest}, at(3), at(8), *at(3)},
		{"past preferred is ignored", ReturnWindow{Latest: latest}, at(-1), at(8), *at(8)},
		{"category default", ReturnWindow{Latest: latest}, nil, at(8), *at(8)},
		{"no category period gives 24 hours, clamped", ReturnWindow{Latest: latest}, nil, nil, latest},
		{"no category period, no cap", ReturnWindow{}, nil, nil, now.Add(24 * time.Hour)},
		{"preferred clamped to latest", ReturnWindow{Latest: latest}, at(72), nil, latest},
		{"latest already past never yields a past due date", ReturnWindow{Latest: now.Add(-time.Minute)}, nil, at(8), now},
	}
	for _, tc := range cases {
		if got := chooseDueAt(now, tc.w, tc.preferred, tc.category); !got.Equal(tc.want) {
			t.Errorf("%s: chooseDueAt = %v, want %v", tc.name, got, tc.want)
		}
	}
}
