package checkout

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/hito-hospital/hdms/internal/modules/checkout/checkoutapi"
	"github.com/hito-hospital/hdms/internal/platform/settings"
)

func TestTTLForState(t *testing.T) {
	cases := []struct {
		state checkoutapi.SessionState
		want  time.Duration
	}{
		{checkoutapi.StateIdle, 45 * time.Second},
		{checkoutapi.StateAwaitingUser, 45 * time.Second},
		{checkoutapi.StateAwaitingDevice, 25 * time.Second},
		{checkoutapi.StateReady, 25 * time.Second},
	}
	for _, c := range cases {
		if got := ttlFor(c.state); got != c.want {
			t.Errorf("ttlFor(%s) = %v, want %v", c.state, got, c.want)
		}
	}
}

type fakeSettings struct {
	st  settings.Settings
	err error
}

func (f fakeSettings) GetSettings(context.Context) (settings.Settings, error) { return f.st, f.err }

func TestServiceTTLForAppliesPolicyToEveryState(t *testing.T) {
	policy := func(secs int) settings.Settings {
		return settings.Settings{Policy: settings.PolicySettings{SessionIdleTimeoutSeconds: secs}}
	}
	states := []checkoutapi.SessionState{
		checkoutapi.StateIdle, checkoutapi.StateAwaitingUser,
		checkoutapi.StateAwaitingDevice, checkoutapi.StateReady,
	}
	cases := []struct {
		name     string
		settings SettingsReader
		want     func(checkoutapi.SessionState) time.Duration
	}{
		{"configured seconds", fakeSettings{st: policy(300)}, func(checkoutapi.SessionState) time.Duration { return 300 * time.Second }},
		{"zero means never", fakeSettings{st: policy(0)}, func(checkoutapi.SessionState) time.Duration { return neverExpires }},
		{"unreadable policy falls back", fakeSettings{err: errors.New("down")}, ttlFor},
		{"no settings dependency falls back", nil, ttlFor},
	}
	for _, c := range cases {
		svc := &Service{deps: Deps{Settings: c.settings}}
		for _, state := range states {
			if got := svc.ttlFor(context.Background(), state); got != c.want(state) {
				t.Errorf("%s: ttlFor(%s) = %v, want %v", c.name, state, got, c.want(state))
			}
		}
	}
}
