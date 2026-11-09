package sctp

// SetupResourceRequirements is a versioned logical integration ledger, not an
// allocator/RSS guarantee. Requires one reader, one writer, one Accept caller,
// no queued I/O, serialized bounded deadline updates and controlled standard
// contexts. Extra waiters/context reference graphs are not covered. Go runtime,
// allocator/GC/stacks, kernel queues and DNS internals are excluded.
//
// ConnectionBytes includes IORequirements scratch and one fixed Notification;
// caller payload/OOB/notification/discard buffers and application state are extra.
// ListenerBytes excludes accepted connections. EstablishBytes includes the
// establishing connection and address work concurrently, but excludes shared
// Dialer/Listener. Reserve DialerBytes+max(EstablishBytes,ConnectionBytes) per
// candidate, plus ListenerBytes per listener. Old connections retiring, new
// candidates and caller-owned notification tails must be charged simultaneously.
// Release operation reservations only after the operation and hooks have returned.
type SetupResourceRequirements struct {
	LedgerVersion                                               string
	DialerBytes, ListenerBytes, EstablishBytes, ConnectionBytes uint64
	IORequirements                                              IORequirements
}
