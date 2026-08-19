package machine

// classify derives the transition table's row axis from a resolved scan
// and the session snapshot it arrived against. It is itself table-tested
// (TestClassify) — this is where "same user" vs "other user" is decided,
// and getting it wrong silently turns a rejection into a return.
func classify(snap Snapshot, in Input) InputClass {
	switch in.Kind {
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
			if snap.UserID != "" && in.HolderUserID == snap.UserID {
				return ClassDeviceOnLoanSameUser
			}
			return ClassDeviceOnLoanOtherUser
		default: // "available"
			return ClassDeviceAvailable
		}

	default:
		return ClassUnknown
	}
}
