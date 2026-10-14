package machine

import "testing"

func TestAdditionalUserCardsDoNotChangeSessionUser(t *testing.T) {
	for _, state := range []SessionState{AwaitingDevice, Ready} {
		for _, status := range []string{"active", "suspended", "archived"} {
			t.Run(state.String()+"/"+status, func(t *testing.T) {
				d := Decide(state, Snapshot{UserID: "first-user"}, Input{Kind: KindUser, UserID: "other-user", UserStatus: status})
				if d.Action != ActionNone || d.NextState != state {
					t.Fatalf("additional card changed session: action=%s state=%s", d.Action, d.NextState)
				}
				if d.MessageArgs["userId"] != "first-user" {
					t.Fatalf("ignored card changed displayed user: %+v", d.MessageArgs)
				}
			})
		}
	}
}

func TestPendingDeviceRefusalKeepsIdentifiedUserSessionReady(t *testing.T) {
	for _, snap := range []Snapshot{
		{PendingDeviceID: "held", PendingDeviceHolderID: "other-user"},
		{PendingDeviceID: "reserved", PendingDeviceReservedForUserID: "other-user"},
	} {
		d := Decide(AwaitingUser, snap, Input{Kind: KindUser, UserID: "first-user", UserStatus: "active"})
		if d.Action != ActionReject || d.NextState != Ready || !d.ClearPending {
			t.Fatalf("refusal prevented further devices: %+v", d)
		}
	}
}

func TestInvalidCardDoesNotDiscardPendingDevices(t *testing.T) {
	for _, in := range []Input{
		{Kind: KindUser, UserID: "suspended", UserStatus: "suspended"},
		{Kind: KindUser, UserID: "archived", UserStatus: "archived"},
		{Kind: KindUnknown}, {Kind: KindUnbound}, {Kind: KindRevoked},
	} {
		d := Decide(AwaitingUser, Snapshot{PendingDeviceID: "device"}, in)
		if d.Action != ActionReject || d.NextState != AwaitingUser || d.ClearPending {
			t.Fatalf("invalid card discarded pending device: %+v", d)
		}
	}
}
