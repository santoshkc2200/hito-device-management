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
		{checkoutapi.StateIdle, idleTTL},
		{checkoutapi.StateAwaitingUser, idleTTL},
		{checkoutapi.StateAwaitingDevice, activeTTL},
		{checkoutapi.StateReady, activeTTL},
	}
	for _, c := range cases {
		if got := ttlFor(c.state); got != c.want {
			t.Errorf("ttlFor(%s) = %v, want %v", c.state, got, c.want)
		}
	}
}
