package machine

import "testing"

func TestClassify(t *testing.T) {
	cases := []struct {
		name string
		snap Snapshot
		in   Input
		want InputClass
	}{
		{"unbound", Snapshot{}, Input{Kind: KindUnbound}, ClassUnbound},
		{"unknown", Snapshot{}, Input{Kind: KindUnknown}, ClassUnknown},
		{"revoked", Snapshot{}, Input{Kind: KindRevoked}, ClassRevoked},

		{"user active, no session user", Snapshot{}, Input{Kind: KindUser, UserID: "u1", UserStatus: "active"}, ClassUserActive},
		{"user active, different from session user", Snapshot{UserID: "u1"}, Input{Kind: KindUser, UserID: "u2", UserStatus: "active"}, ClassUserActive},
		{"user same as session user", Snapshot{UserID: "u1"}, Input{Kind: KindUser, UserID: "u1", UserStatus: "active"}, ClassUserSame},
		{"user suspended overrides sameness", Snapshot{UserID: "u1"}, Input{Kind: KindUser, UserID: "u1", UserStatus: "suspended"}, ClassUserSuspended},
		{"user archived", Snapshot{}, Input{Kind: KindUser, UserID: "u1", UserStatus: "archived"}, ClassUserArchived},

		{"device available", Snapshot{}, Input{Kind: KindDevice, DeviceStatus: "available"}, ClassDeviceAvailable},
		{"device maintenance", Snapshot{}, Input{Kind: KindDevice, DeviceStatus: "maintenance"}, ClassDeviceUnavailable},
		{"device retired", Snapshot{}, Input{Kind: KindDevice, DeviceStatus: "retired"}, ClassDeviceUnavailable},
		{"device lost", Snapshot{}, Input{Kind: KindDevice, DeviceStatus: "lost"}, ClassDeviceUnavailable},
		{
			"device on loan to the session user",
			Snapshot{UserID: "u1"},
			Input{Kind: KindDevice, DeviceStatus: "on_loan", HolderUserID: "u1"},
			ClassDeviceOnLoanSameUser,
		},
		{
			"device on loan to someone else",
			Snapshot{UserID: "u1"},
			Input{Kind: KindDevice, DeviceStatus: "on_loan", HolderUserID: "u2"},
			ClassDeviceOnLoanOtherUser,
		},
		{
			"device on loan, no session user yet",
			Snapshot{},
			Input{Kind: KindDevice, DeviceStatus: "on_loan", HolderUserID: "u2"},
			ClassDeviceOnLoanOtherUser,
		},
		{
			"device duplicate takes priority over availability",
			Snapshot{},
			Input{Kind: KindDevice, DeviceStatus: "available", SameAsPendingWithin3s: true},
			ClassDeviceDuplicate,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := classify(c.snap, c.in); got != c.want {
				t.Errorf("classify() = %q, want %q", got, c.want)
			}
		})
	}
}

func TestResolveAction(t *testing.T) {
	cases := []struct {
		name            string
		custodyHolderID string
		actorID         string
		want            Action
	}{
		{"no holder is a borrow", "", "u1", ActionBorrow},
		{"holder is the actor is a return", "u1", "u1", ActionReturn},
		{"holder is someone else is a rejection", "u2", "u1", ActionReject},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ResolveAction(c.custodyHolderID, c.actorID); got != c.want {
				t.Errorf("ResolveAction(%q, %q) = %q, want %q", c.custodyHolderID, c.actorID, got, c.want)
			}
		})
	}
}
