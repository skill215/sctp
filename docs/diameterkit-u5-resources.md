# U5 resource ledger and candidate gates (batch 62)

This is a versioned integration ledger for the incremental message-mode candidate,
not a new allocator/API, a process RSS cap or the independent v1 rewrite.
The public package still allows arbitrary concurrent callers. A bounded adapter
must provide one reader and one writer per connection, no queued upstream I/O,
standard-library contexts under its control, bounded configuration and a single
owned parsed-notification value. Raw-fd or arbitrary custom-context behavior is
outside this ledger. Connection-level cancellation must not retain an unbounded
parent context/value graph.

## Established connection: logical accounting

TestU5ResourceLedger emits sizeof values for the compiled candidate. On
Go1.26.10/Linux/amd64: SCTPConn96, socketIO152, MessageOptions12, os.File handle8,
socketOperation40 and Notification168 bytes. The owned resident value subtotal
is268. ReadScratch480 + WriteScratch280 + Peek8 + two operations80 + one
Notification168 =1016 simultaneous working bytes. Scratch already includes its
result/metadata copies, and is not counted again. The immutable options shared
by accepted connections are conservatively charged per connection.

The integration policy adds explicit logical allowances, not measured allocator
sizes: seven channel slots at256 bytes (two gates, close, two changed-deadline
channels, two operation completion channels), four standard-context/hook slots
at512 (two operation contexts, two AfterFunc registrations and their bounded
state), and one file/poller slot at1024. Subtotal4864; with value/work bytes the
accounted sum is6148, rounded to an8192-byte fixed logical integration reserve.
The test fails if source-value/workspace changes exceed that reserve. Slot
allowances must be reassessed with the pinned Go/source version; they do not
claim to bound hidden standard-library/runtime allocation exactly.

At most two operation owners and two AfterFunc callbacks are live. The normal
adapter has zero gate waiters and zero gate-acquisition timers. Context/deadline
runtime timers and poller timer state belong to the declared context/file slots;
Go scheduling, stacks, allocator/GC overhead and kernel socket/send/receive
memory remain outside the byte ledger and require separate capacity planning.
Stopped/replaced objects awaiting GC do not become a claimed RSS bound.
There is no permanent per-connection worker in this package. Completion waits
for a started cancellation callback before releasing the gate/buffers.

With Q external queued callers, there may additionally be Q operation values,
Q timers, Q held changed-channel generations and their caller contexts/stacks.
The fixed8192 integration reserve does not authorize Q>0. Arbitrarily frequent
concurrent deadline setters/custom contexts likewise have no finite universal
allocation promise. The adapter must serialize its own deadline management.

Adapter per-connection accounting is additive:

    8192 + adapter fixed state + prefix20 + NotificationMax + OOBMin
         + discard scratch + independent control RX/TX + emergency slots

The upstream read/write/peek workspace and one Notification value are already
inside8192. Caller frame buffers remain in their business/control reservations;
no second whole frame is added. Notifications cannot borrow business space or
grow NotificationMax. Tail is borrowed and must not outlive the next read; any
owned diagnostic copy requires its own explicit reservation.

## Listener and establishment overlap

Listener values are SCTPListener40 + socketIO152 + options12 + os.File8 =212.
Use the same8192 logical fixed envelope per live listener or establishing socket
for conservative accounting; it is not free merely because no business Peer is
Ready. A listener and every accepted candidate coexist and require separate
slots. On successful dial, transfer the establishment charge to the connected
slot; do not release and reacquire around handoff. Failure releases it only after
owned socket/poller operations settle. A prebuilt external socket is accounted
from adapter takeover, not retroactively before it existed.

Input/address copies need a separate finite setup reservation. For N total
explicit local+remote addresses, Z total zone-text bytes and D bytes of bounded
resolution input, a conservative library-value allowance is
`64 + 96*N + Z + 2*D + 16*(N+2)` on this audited64-bit candidate: two address
headers; net.IPAddr/IP/raw-sockaddr data and alignment per address; text and
split-string indexing. SocketConfig's32-byte value and fixed setup records fit
the fixed envelope. Original Node configuration is separately charged once.
The adapter must validate N/Z/D before copying or resolving; the general public
resolver is not a hostile-input quota service. Go DNS/zone resolver internals
and caller-owned context graphs remain external infrastructure, not hidden in a
claim of bounded library payload storage. Setup budgets must account for all
simultaneously admitted dial/accept slots, not just open connections.

## ABI, lifecycle and release gates

TestU5SelectedABI checks SNDRCV offsets/size, notification header, sockaddr and
CMSG layout. Native tests already exercise all five selected notifications;
TestU5MessageCancellationRetiresOwners repeats100 admitted reads with cancel,
immediate buffer reuse and subsequent real send/read, asserting no owner/gate
remains after return. This is direct lifecycle evidence, not a heuristic global
goroutine count. The full existing suite/race and checkptr remain required.

The native CI script now runs a distinct CGO_ENABLED=0 suite and includes the
new ParseNotification fuzz target in its existing1,000,000-execution policy.
The cross-architecture workflow now includes the pure message ABI/parser tests;
QEMU syscall limitations still mean emulator parsing is not native SCTP I/O.
Independent consumer compilation now references all new APIs. CI execution on
the exact candidate's Ubuntu22.04/24.04, Go1.26.x/1.27.x and existing architecture
matrix remains a publication gate, not something inferred from local compilation.

A deterministic file-proxy module package is tested by diameterkit without a
replace directive. Its v0.0.0-diameterkit.u5.62 label is offline-only, with source
manifest/zip hash/Go module sums, not a tag, published version or upstream commit.
The independent consumer uses its own module cache and performs real message
I/O. Production must lock the subsequently reviewed/published exact candidate;
a local install proof alone does not satisfy provenance or the remote matrix.

Remaining external gates are explicit: exact-candidate upstream review/commit
and release pipeline, native matrix results (including required IPv6 namespace
cases), then diameterkit SCTP adapter budget/admission/Peer integration and DRA
acceptance. No new notification families or protocol design rounds are required.
