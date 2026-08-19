package machine

import "testing"

func TestTableIsComplete(t *testing.T) {
	table := Table()
	for _, s := range allStates {
		classes, ok := table[s]
		if !ok {
			t.Fatalf("no rules at all for state %q", s)
		}
		for _, c := range allClasses {
			if _, ok := classes[c]; !ok {
				t.Errorf("missing rule for state=%q class=%q", s, c)
			}
		}
	}
}

// TestMatrix walks every cell of docs/04-scanning-and-checkout-flows.md's
// per-state tables as a named subtest, asserting the Action and NextState
// the table actually produces. The two AwaitingUser × {ClassUserActive,
// ClassUserSame} cells are resolved dynamically (resolvePendingAgainstUser)
// and are covered by TestResolvePendingAgainstUser instead, since their
// outcome depends on the snapshot, not just the cell.
func TestMatrix(t *testing.T) {
	type cell struct {
		state      SessionState
		class      InputClass
		wantAction Action
		wantNext   SessionState
	}
	cells := []cell{
		// idle
		{Idle, ClassDeviceAvailable, ActionHoldDevice, AwaitingUser},
		{Idle, ClassDeviceOnLoanSameUser, ActionHoldDevice, AwaitingUser},
		{Idle, ClassDeviceOnLoanOtherUser, ActionHoldDevice, AwaitingUser},
		{Idle, ClassDeviceUnavailable, ActionReject, Idle},
		{Idle, ClassDeviceDuplicate, ActionDuplicate, Idle},
		{Idle, ClassUserActive, ActionSetUser, AwaitingDevice},
		{Idle, ClassUserSuspended, ActionReject, Idle},
		{Idle, ClassUserArchived, ActionReject, Idle},
		{Idle, ClassUnbound, ActionReject, Idle},
		{Idle, ClassUnknown, ActionReject, Idle},
		{Idle, ClassRevoked, ActionReject, Idle},
		{Idle, ClassTimeout, ActionExpire, Idle},

		// awaiting_user
		{AwaitingUser, ClassDeviceAvailable, ActionReplacePending, AwaitingUser},
		{AwaitingUser, ClassDeviceOnLoanSameUser, ActionReplacePending, AwaitingUser},
		{AwaitingUser, ClassDeviceOnLoanOtherUser, ActionReplacePending, AwaitingUser},
		{AwaitingUser, ClassDeviceUnavailable, ActionReject, AwaitingUser},
		{AwaitingUser, ClassDeviceDuplicate, ActionDuplicate, AwaitingUser},
		{AwaitingUser, ClassUserSuspended, ActionReject, Idle},
		{AwaitingUser, ClassUserArchived, ActionReject, Idle},
		{AwaitingUser, ClassUnbound, ActionReject, Idle},
		{AwaitingUser, ClassUnknown, ActionReject, Idle},
		{AwaitingUser, ClassRevoked, ActionReject, Idle},
		{AwaitingUser, ClassTimeout, ActionExpire, Idle},

		// awaiting_device and ready share rules
		{AwaitingDevice, ClassDeviceAvailable, ActionBorrow, Ready},
		{AwaitingDevice, ClassDeviceOnLoanSameUser, ActionReturn, Ready},
		{AwaitingDevice, ClassDeviceOnLoanOtherUser, ActionReject, AwaitingDevice},
		{AwaitingDevice, ClassDeviceUnavailable, ActionReject, AwaitingDevice},
		{AwaitingDevice, ClassDeviceDuplicate, ActionDuplicate, AwaitingDevice},
		{AwaitingDevice, ClassUserActive, ActionSwitchUser, AwaitingDevice},
		{AwaitingDevice, ClassUserSame, ActionNone, AwaitingDevice},
		{AwaitingDevice, ClassUserSuspended, ActionReject, AwaitingDevice},
		{AwaitingDevice, ClassUserArchived, ActionReject, AwaitingDevice},
		{AwaitingDevice, ClassUnbound, ActionReject, AwaitingDevice},
		{AwaitingDevice, ClassUnknown, ActionReject, AwaitingDevice},
		{AwaitingDevice, ClassRevoked, ActionReject, AwaitingDevice},
		{AwaitingDevice, ClassTimeout, ActionExpire, Idle},

		{Ready, ClassDeviceAvailable, ActionBorrow, Ready},
		{Ready, ClassDeviceOnLoanSameUser, ActionReturn, Ready},
		{Ready, ClassDeviceOnLoanOtherUser, ActionReject, Ready},
		{Ready, ClassDeviceUnavailable, ActionReject, Ready},
		{Ready, ClassDeviceDuplicate, ActionDuplicate, Ready},
		{Ready, ClassUserActive, ActionSwitchUser, AwaitingDevice},
		{Ready, ClassUserSame, ActionNone, Ready},
		{Ready, ClassUserSuspended, ActionReject, Ready},
		{Ready, ClassUserArchived, ActionReject, Ready},
		{Ready, ClassUnbound, ActionReject, Ready},
		{Ready, ClassUnknown, ActionReject, Ready},
		{Ready, ClassRevoked, ActionReject, Ready},
		{Ready, ClassTimeout, ActionExpire, Idle},
	}

	for _, c := range cells {
		t.Run(string(c.state)+"/"+string(c.class), func(t *testing.T) {
			rule, ok := Table()[c.state][c.class]
			if !ok {
				t.Fatalf("no rule for state=%q class=%q", c.state, c.class)
			}
			got := rule.Apply(Snapshot{}, Input{})
			if got.Action != c.wantAction {
				t.Errorf("Action = %q, want %q", got.Action, c.wantAction)
			}
			if got.NextState != c.wantNext {
				t.Errorf("NextState = %q, want %q", got.NextState, c.wantNext)
			}
		})
	}
}

func TestPendingDeviceReleasedOnRefusal(t *testing.T) {
	for _, class := range []InputClass{ClassUnbound, ClassUnknown, ClassRevoked, ClassUserSuspended, ClassUserArchived} {
		t.Run(string(class), func(t *testing.T) {
			d := Decide(AwaitingUser, Snapshot{PendingDeviceID: "dev-1"}, inputFor(class))
			if d.NextState != Idle {
				t.Errorf("NextState = %q, want idle", d.NextState)
			}
			if !d.ClearPending {
				t.Error("ClearPending = false, want true — a refusal from awaiting_user must release the pending device")
			}
		})
	}
}

// inputFor builds a minimal Input that classify() maps to class, for tests
// that only care about the resulting Decision, not the exact scanned
// values.
func inputFor(class InputClass) Input {
	switch class {
	case ClassUnbound:
		return Input{Kind: KindUnbound}
	case ClassUnknown:
		return Input{Kind: KindUnknown}
	case ClassRevoked:
		return Input{Kind: KindRevoked}
	case ClassUserSuspended:
		return Input{Kind: KindUser, UserStatus: "suspended"}
	case ClassUserArchived:
		return Input{Kind: KindUser, UserStatus: "archived"}
	case ClassUserActive:
		return Input{Kind: KindUser, UserStatus: "active"}
	case ClassDeviceUnavailable:
		return Input{Kind: KindDevice, DeviceStatus: "maintenance"}
	case ClassDeviceAvailable:
		return Input{Kind: KindDevice, DeviceStatus: "available"}
	default:
		return Input{}
	}
}

func TestResolvePendingAgainstUser(t *testing.T) {
	t.Run("no holder is a borrow", func(t *testing.T) {
		d := Decide(AwaitingUser, Snapshot{PendingDeviceID: "dev-1", PendingDeviceStatus: "available"},
			Input{Kind: KindUser, UserID: "user-1", UserStatus: "active"})
		if d.Action != ActionBorrow || d.NextState != Ready || !d.ClearPending {
			t.Errorf("Decision = %+v, want Action=borrow NextState=ready ClearPending=true", d)
		}
	})
	t.Run("holder is the scanning user is a return", func(t *testing.T) {
		d := Decide(AwaitingUser, Snapshot{PendingDeviceID: "dev-1", PendingDeviceStatus: "on_loan", PendingDeviceHolderID: "user-1"},
			Input{Kind: KindUser, UserID: "user-1", UserStatus: "active"})
		if d.Action != ActionReturn || d.NextState != Ready || !d.ClearPending {
			t.Errorf("Decision = %+v, want Action=return NextState=ready ClearPending=true", d)
		}
	})
	t.Run("holder is someone else is a rejection", func(t *testing.T) {
		d := Decide(AwaitingUser, Snapshot{PendingDeviceID: "dev-1", PendingDeviceStatus: "on_loan", PendingDeviceHolderID: "user-2"},
			Input{Kind: KindUser, UserID: "user-1", UserStatus: "active"})
		if d.Action != ActionReject || d.NextState != Idle || !d.ClearPending {
			t.Errorf("Decision = %+v, want Action=reject NextState=idle ClearPending=true", d)
		}
		if d.MessageKey != MsgDeviceHeldByOther {
			t.Errorf("MessageKey = %q, want %q", d.MessageKey, MsgDeviceHeldByOther)
		}
	})
}
