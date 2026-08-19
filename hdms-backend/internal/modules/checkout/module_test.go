package checkout

import (
	"testing"
	"time"

	"github.com/hito-hospital/hdms/internal/modules/checkout/checkoutapi"
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
