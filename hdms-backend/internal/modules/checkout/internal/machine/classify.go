package machine

// classify derives the transition table's row axis from a resolved scan
// and the session snapshot it arrived against. It is itself table-tested
// (TestClassify) — this is where "same user" vs "other user" is decided,
// and getting it wrong silently turns a rejection into a return.
func classify(snap Snapshot, in Input) InputClass {
	switch in.Kind {
	case KindTimeout:
		return ClassTimeout
	case KindUnbound:
		return ClassUnbound
	case KindUnknown:
		return ClassUnknown
	case KindRevoked:
		return ClassRevoked

	case KindUser:
		switch in.UserStatus {
		case "suspended":
			return ClassUserSuspended
		case "archived":
			return ClassUserArchived
		}
		if snap.UserID != "" && snap.UserID == in.UserID {
			return ClassUserSame
		}
		return ClassUserActive

	case KindDevice:
		if in.SameAsPendingWithin3s {
			return ClassDeviceDuplicate
		}
		switch in.DeviceStatus {
		case "maintenance", "retired", "lost":
			return ClassDeviceUnavailable
		case "on_loan":
			// Custody is decided before any reservation: an open loan
			// means someone is holding this device now, and a claim on a
			// future interval does not change that. This ordering is what
			// keeps an overrunning borrower from being punished by the
			// screen — they return exactly as they always did (6.4a).
			if snap.UserID != "" && in.HolderUserID == snap.UserID {
				return ClassDeviceOnLoanSameUser
			}
			return ClassDeviceOnLoanOtherUser
		default: // "available"
			// Phase 6.4c. ReservedForUserID is "" unless the caller found
			// a reservation actually in force — inside the window or its
			// pre-window — so an unreserved device, and a device reserved
			// for some later time, both fall straight through to
			// ClassDeviceAvailable and behave as they did before.
			if in.ReservedForUserID != "" {
				// With no user identified yet snap.UserID is "", so this
				// is "by other" and the Idle/AwaitingUser rules hold the
				// device pending rather than refusing it — the refusal
				// comes later, when the scanner turns out not to be the
				// reserver, exactly as it does for custody.
				if snap.UserID != "" && snap.UserID == in.ReservedForUserID {
					return ClassDeviceReservedBySelf
				}
				return ClassDeviceReservedByOther
			}
			return ClassDeviceAvailable
		}

	default:
		return ClassUnknown
	}
}
