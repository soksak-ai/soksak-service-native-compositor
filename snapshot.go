package nativesurface

import (
	"fmt"
	"math"
	"sort"
	"sync"
	"time"
	"unsafe"
)

type SurfaceKind string

type Frame struct {
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

// SurfaceSource is opaque to the compositor. The native surface plugin owns
// the meaning of its declared keys.
type SurfaceSource map[string]string

type Surface struct {
	ID         string        `json:"id"`
	Generation uint64        `json:"generation"`
	Kind       SurfaceKind   `json:"kind"`
	Frame      Frame         `json:"frame"`
	Visible    bool          `json:"visible"`
	Alpha      float64       `json:"alpha"`
	Layer      int           `json:"layer"`
	Source     SurfaceSource `json:"source"`
}

type Snapshot struct {
	// Window names the window whose document declared these surfaces.
	//
	// A surface is attached to one window's content view and its frame is in that document's
	// coordinates. Without the name a host with two windows attaches every surface to whichever
	// one it happened to hand over, and the frames are read against the wrong box.
	Window   string `json:"window"`
	Sequence uint64 `json:"sequence"`
	// Interactive is the layout system's explicit gesture phase. It is carried with the whole
	// inventory so each native kind owns how its live surface is presented during that phase.
	Interactive bool      `json:"interactive"`
	Surfaces    []Surface `json:"surfaces"`
	// SentAtUnixMs is when the document handed this over, by its own wall clock.
	//
	// A receipt says how long the backend held the commit and a caller measures the whole
	// round trip, and the difference between those two is time nobody could name. Measured
	// 2026-08-17 in a window changing its layout: 40ms round trip, 0.2ms of native work, and
	// no reading that said where the other 39.8 went. Stamped here, the crossing can be
	// subtracted from the round trip and what is left is the bridge.
	//
	// Zero is a caller that does not stamp, and the receipt then reports no crossing rather
	// than a number made from two clocks that never met.
	SentAtUnixMs float64 `json:"sentAtUnixMs"`
}

type AppliedSurface struct {
	ID         string `json:"id"`
	Generation uint64 `json:"generation"`
	Frame      Frame  `json:"frame"`
	// Settled is the native view's raw layout frame when presentation differs from layout during
	// an interactive phase. Nil means this native kind has no separate settled geometry.
	Settled                   *Frame  `json:"settled,omitempty"`
	LayerContentsRedrawPolicy int     `json:"layerContentsRedrawPolicy"`
	LayerContentsPlacement    int     `json:"layerContentsPlacement"`
	Visible                   bool    `json:"visible"`
	Alpha                     float64 `json:"alpha"`
	Layer                     int     `json:"layer"`
	// Window is the window the backend read this surface out of after
	// attaching it — not the one it was told to use. A backend fills it from
	// the native object; the compositor compares.
	//
	// Never serialised. A page has no use for a native handle, and one on the
	// wire is a value a caller can be tempted to send back.
	Window unsafe.Pointer `json:"-"`
	// Misparented reports that the surface is in a different window from the
	// one that declared it. The compositor sets it; a backend's own value is
	// overwritten.
	//
	// Every other number here is a fact about a rectangle inside some window,
	// so all of them read correct while the rectangle sits in a window nobody
	// is looking at. Measured 2026-08-16: declared and applied frames agreed to
	// zero drift, visible was true, alpha was 1, and the pane on screen was
	// empty.
	Misparented bool `json:"misparented"`
}

type Receipt struct {
	Sequence uint64           `json:"sequence"`
	Accepted bool             `json:"accepted"`
	Surfaces []AppliedSurface `json:"surfaces"`
	// AppliedAtUnixMs is the wall-clock instant immediately after the backend applied this inventory.
	// It is recorded here, where Apply actually returns. A frontend response timestamp includes the
	// return bridge and cannot say when the native view moved.
	AppliedAtUnixMs float64 `json:"appliedAtUnixMs"`
	// AppliedMs is how long the backend held this commit — the native work alone, without the
	// bridge that carried the request or the wait for a thread that was busy with something else.
	//
	// A caller measuring a commit from the other side of the bridge measures both, and the two ask
	// different questions: work the application does costs this, and time it spends waiting is the
	// window being busy elsewhere. Measured 2026-08-17, a window stopped drawing for exactly as long
	// as its commits took, and nothing could tell those two apart.
	AppliedMs float64 `json:"appliedMs"`
	// CarriedMs is how long the commit took to reach the backend from the document that sent
	// it — the bridge, the queue behind it, and whatever the main thread was doing instead.
	//
	// -1 when the caller stamped nothing. A caller that stamps gets the round trip split in
	// two: this, and AppliedMs. What is left over is the answer's way back.
	CarriedMs float64 `json:"carriedMs"`
}

type Backend interface {
	Apply(window unsafe.Pointer, snapshot Snapshot) ([]AppliedSurface, error)
	// Deliver hands a message to whichever surface kind this backend implements.
	//
	// A browser is told to go back, a video surface to seek. Those verbs belong to the kind: a
	// compositor that knew them would need editing for every kind added, which is the lock-in the
	// substrate exists to prevent. So the message travels closed — the compositor validates who it
	// is for and forwards it without reading it.
	Deliver(id string, message map[string]any) (map[string]any, error)
}

// Service places declared surfaces in the windows that declared them.
//
// State is per window, all of it. One window's sequence counter, inventory and
// last commit say nothing about another's: a shared counter answers the second
// window's first commit as stale behind the first window's tenth, and a shared
// inventory answers a question about one window with every window's surfaces.
type Service struct {
	mu sync.Mutex
	// windows resolves a window name to its native handle. Nil, or an unknown
	// name, is "that window is not ready" — which is not the same as a window
	// holding no surfaces.
	windows func(name string) unsafe.Pointer
	// backends is one per surface kind, and the kind on a declaration is what picks it.
	//
	// There was one backend, and the kind was validated non-empty and then never read — a declared
	// fact with no consumer, which is the shape this substrate refuses everywhere else. It also made
	// the seam a claim rather than a seam: whatever that one backend implemented was every kind
	// there was, so a second kind meant editing this file, and a compositor that has to be edited
	// per kind is the lock-in it exists to prevent.
	//
	// Routing by kind reads a declaration rather than inventing an abstraction. The field is already
	// on the wire, already validated, and already stated by the unit that implements it.
	backends  map[SurfaceKind]Backend
	latest    map[string]Receipt
	committed map[string]windowCommit
	history   map[string][]windowCommit
	stopped   bool
}

// Enough for ten seconds at a deliberately hostile 120 native applications per second. Older
// samples cannot participate in the bounded UI trace and are discarded at the writer.
const historyLimit = 1200

// NewService takes one backend per surface kind.
//
// A kind with no backend is refused by name at the declaration that asks for it, rather than at a
// nil dereference or a silent skip: a surface nobody can place is a fact the caller has to be told,
// and a caller that is not told draws an empty pane and concludes the page failed to load.
func NewService(windows func(name string) unsafe.Pointer, backends map[SurfaceKind]Backend) *Service {
	held := make(map[SurfaceKind]Backend, len(backends))
	for kind, backend := range backends {
		held[kind] = backend
	}
	return &Service{
		windows:   windows,
		backends:  held,
		latest:    map[string]Receipt{},
		committed: map[string]windowCommit{},
		history:   map[string][]windowCommit{},
	}
}

// unplaceableLocked names the first kind in a snapshot that no backend implements.
//
// Refused before anything is applied, so a commit is all or none. Half an inventory placed and half
// refused leaves the document's picture and the screen apart, with a receipt that says the commit
// succeeded for what it managed.
func (service *Service) unplaceableLocked(snapshot Snapshot) (SurfaceKind, bool) {
	for _, surface := range snapshot.Surfaces {
		if _, wired := service.backends[surface.Kind]; !wired {
			return surface.Kind, true
		}
	}
	return "", false
}

// applyByKind gives every wired backend its own complete inventory for this window.
//
// Every one of them, not only the kinds present. A backend whose surfaces have all gone has to be
// told, and telling it means an empty list — the inventory is complete or it is not an inventory,
// and a backend skipped this round keeps drawing what the document has already forgotten.
func (service *Service) applyByKind(handle unsafe.Pointer, snapshot Snapshot) ([]AppliedSurface, error) {
	byKind := make(map[SurfaceKind][]Surface, len(service.backends))
	for kind := range service.backends {
		byKind[kind] = nil
	}
	for _, surface := range snapshot.Surfaces {
		byKind[surface.Kind] = append(byKind[surface.Kind], surface)
	}

	kinds := make([]SurfaceKind, 0, len(byKind))
	for kind := range byKind {
		kinds = append(kinds, kind)
	}
	// In one order every time. A map walks differently on every run, and two runs of one build that
	// place surfaces in a different order are two builds as far as any reading of them is concerned.
	sort.Slice(kinds, func(i, j int) bool { return kinds[i] < kinds[j] })

	applied := make([]AppliedSurface, 0, len(snapshot.Surfaces))
	for _, kind := range kinds {
		own := snapshot
		own.Surfaces = byKind[kind]
		result, err := service.backends[kind].Apply(handle, own)
		if err != nil {
			return nil, fmt.Errorf("native surface kind %q: %w", kind, err)
		}
		applied = append(applied, result...)
	}
	return applied, nil
}

// kindOfLocked answers what kind a surface was declared as, from the last commit that declared it.
func (service *Service) kindOfLocked(id string) (SurfaceKind, bool) {
	for _, commit := range service.committed {
		for _, surface := range commit.declared.Surfaces {
			if surface.ID == id {
				return surface.Kind, true
			}
		}
	}
	return "", false
}

// Kinds is every surface kind this compositor can place. It answers what is wired, not what exists.
func (service *Service) Kinds() []SurfaceKind {
	service.mu.Lock()
	defer service.mu.Unlock()
	out := make([]SurfaceKind, 0, len(service.backends))
	for kind := range service.backends {
		out = append(out, kind)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// window answers one window's handle, or nil when it cannot be resolved.
func (service *Service) window(name string) unsafe.Pointer {
	if service.windows == nil {
		return nil
	}
	return service.windows(name)
}

func (service *Service) ServiceName() string { return "wails-service-native-compositor" }

func validFrame(frame Frame) bool {
	values := []float64{frame.X, frame.Y, frame.Width, frame.Height}
	for _, value := range values {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return false
		}
	}
	return frame.Width >= 0 && frame.Height >= 0
}

func validateSnapshot(snapshot Snapshot) error {
	// An unnamed window is refused rather than defaulted. A default sends every
	// surface to one window, which is the shape that put a workspace window's
	// browser inside an orchestrator and reported success.
	if snapshot.Window == "" {
		return fmt.Errorf("native surface snapshot names no window")
	}
	if snapshot.Sequence == 0 {
		return fmt.Errorf("native surface snapshot sequence must be positive")
	}
	seen := make(map[string]struct{}, len(snapshot.Surfaces))
	for _, surface := range snapshot.Surfaces {
		if surface.ID == "" || surface.Generation == 0 {
			return fmt.Errorf("native surface identity is invalid: %q/%d", surface.ID, surface.Generation)
		}
		if surface.Kind == "" {
			return fmt.Errorf("native surface kind is required: %s", surface.ID)
		}
		if !validFrame(surface.Frame) {
			return fmt.Errorf("native surface frame is invalid: %s", surface.ID)
		}
		if math.IsNaN(surface.Alpha) || math.IsInf(surface.Alpha, 0) || surface.Alpha < 0 || surface.Alpha > 1 {
			return fmt.Errorf("native surface alpha is invalid: %s", surface.ID)
		}
		if _, duplicate := seen[surface.ID]; duplicate {
			return fmt.Errorf("duplicate native surface id: %s", surface.ID)
		}
		seen[surface.ID] = struct{}{}
	}
	return nil
}

func (service *Service) Commit(snapshot Snapshot) (Receipt, error) {
	if err := validateSnapshot(snapshot); err != nil {
		return Receipt{}, err
	}
	service.mu.Lock()
	defer service.mu.Unlock()
	if service.stopped {
		return Receipt{}, fmt.Errorf("native surface service is shutting down")
	}
	// This window's counter. A counter shared with other windows answers this
	// window's first commit as stale and leaves it without surfaces for good.
	if snapshot.Sequence <= service.latest[snapshot.Window].Sequence {
		stale := service.latest[snapshot.Window]
		stale.Accepted = false
		return stale, nil
	}
	handle := service.window(snapshot.Window)
	if handle == nil {
		return Receipt{}, fmt.Errorf("native surface window %s is not ready", snapshot.Window)
	}
	if len(service.backends) == 0 {
		return Receipt{}, fmt.Errorf("this compositor has no backend for any surface kind")
	}
	if kind, unplaceable := service.unplaceableLocked(snapshot); unplaceable {
		return Receipt{}, fmt.Errorf(
			"native surface kind %q has no backend in this build — a surface nobody can place is "+
				"refused here rather than left out of the answer, where it reads as a pane that drew nothing",
			kind)
	}
	// Stamped before the work, against the same wall clock the document used.
	carriedMs := float64(-1)
	if snapshot.SentAtUnixMs > 0 {
		carriedMs = float64(time.Now().UnixNano())/1e6 - snapshot.SentAtUnixMs
	}
	startedAt := time.Now()
	applied, err := service.applyByKind(handle, snapshot)
	appliedMs := float64(time.Since(startedAt).Microseconds()) / 1000
	appliedAtUnixMs := float64(time.Now().UnixNano()) / 1e6
	if err != nil {
		// A refused attempt is recorded rather than forgotten. Without it the
		// last healthy inventory keeps answering, and every reading reports a
		// healthy native layer.
		failed := service.committed[snapshot.Window]
		failed.failure = err.Error()
		failed.failedSequence = snapshot.Sequence
		service.committed[snapshot.Window] = failed
		return Receipt{}, err
	}
	// The one coordinate the backend cannot restate: which window the surface is
	// actually in. A backend that names nothing is reported misparented rather
	// than trusted — silence is what a backend that never learned to read the
	// window back produces, and reading that as agreement is how this stayed
	// invisible for as long as it did.
	for index := range applied {
		applied[index].Misparented = applied[index].Window != handle
	}
	sort.Slice(applied, func(i, j int) bool { return applied[i].ID < applied[j].ID })
	receipt := Receipt{
		Sequence:        snapshot.Sequence,
		Accepted:        true,
		Surfaces:        applied,
		AppliedAtUnixMs: appliedAtUnixMs,
		AppliedMs:       appliedMs,
		CarriedMs:       carriedMs,
	}
	service.latest[snapshot.Window] = receipt
	commit := windowCommit{declared: snapshot, applied: receipt}
	service.committed[snapshot.Window] = commit
	service.appendHistoryLocked(snapshot.Window, commit)
	return receipt, nil
}

func (service *Service) appendHistoryLocked(window string, commit windowCommit) {
	history := append(service.history[window], commit)
	if len(history) > historyLimit {
		history = append([]windowCommit(nil), history[len(history)-historyLimit:]...)
	}
	service.history[window] = history
}

// Status answers one window's applied inventory.
func (service *Service) Status(window string) Receipt {
	service.mu.Lock()
	defer service.mu.Unlock()
	return service.latest[window]
}

// Deliver forwards a message to the backend that owns a surface.
//
// The inventory is the only record of what exists, so a message for an id nobody declared is
// refused here. Forwarding it would ask the backend to invent a surface, and a backend that obliges
// holds one the compositor does not know about — the undeclared surface a ledger-only check never
// sees, because the ledger is what it walks.
func (service *Service) Deliver(id string, message map[string]any) (map[string]any, error) {
	service.mu.Lock()
	defer service.mu.Unlock()
	if service.stopped {
		return nil, fmt.Errorf("native surface %s cannot be driven: this compositor has shut down", id)
	}
	// Every window's inventory. A surface id is unique across the application,
	// so the window it is in is not something a caller has to know to drive it.
	for _, receipt := range service.latest {
		for _, surface := range receipt.Surfaces {
			if surface.ID != id {
				continue
			}
			// Which backend gets it comes from the declaration, because that is where a kind is
			// stated. An applied surface reports where it landed and not what it is.
			kind, declared := service.kindOfLocked(id)
			if !declared {
				return nil, fmt.Errorf(
					"native surface %s was applied and no declaration names its kind — nothing can "+
						"be told which backend it belongs to", id)
			}
			backend, wired := service.backends[kind]
			if !wired {
				return nil, fmt.Errorf(
					"native surface %s is of kind %q and this build has no backend for it", id, kind)
			}
			return backend.Deliver(id, message)
		}
	}
	return nil, fmt.Errorf("native surface %s is in no window's applied inventory", id)
}

func (service *Service) ServiceShutdown() error {
	_, _, err := service.Drain()
	return err
}

// Drain takes every surface down and answers how many came down, how many are
// still held, and what stopped it.
//
// Two numbers because they are two claims. "Four came down" and "none are left"
// are different facts, and the second is the one that matters: a surface still
// held when the process exits is a native child outliving its parent. A drain
// that worked and reported nothing could not be part of the receipt
// `app_shutdown_prepare` answers, which is why the command that quits the
// application was declared unserved (measured 2026-08-16: `sok
// app.shutdown.commit` answered INTERNAL).
//
// Idempotent, and a second call answers zero: a count that repeated itself
// would report work that did not happen.
//
// Every window is emptied, and a window that held nothing needs no window
// handle to be emptied — a drain that refused on an already-closed window would
// leave the windows after it in the map still holding surfaces.
func (service *Service) Drain() (drained int, remaining int, err error) {
	service.mu.Lock()
	defer service.mu.Unlock()
	if service.stopped {
		return 0, service.heldLocked(), nil
	}
	service.stopped = true

	// Sorted, so two runs of one shutdown empty in the same order and a failure
	// names the same window twice rather than a different one each time.
	names := make([]string, 0, len(service.latest))
	for name := range service.latest {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		receipt := service.latest[name]
		if len(receipt.Surfaces) == 0 {
			continue
		}
		handle := service.window(name)
		if handle == nil {
			return drained, service.heldLocked(),
				fmt.Errorf("native surface window %s is unavailable during shutdown", name)
		}
		if len(service.backends) == 0 {
			return drained, service.heldLocked(),
				fmt.Errorf("this compositor has no backend for any surface kind during shutdown")
		}
		sequence := receipt.Sequence + 1
		// Every backend is told, with an empty inventory each. Shutdown is the case where every kind
		// has to hear that nothing is declared any more, and a backend skipped here keeps its
		// surfaces on a window the application is done with.
		applied, applyErr := service.applyByKind(handle, Snapshot{Window: name, Sequence: sequence})
		if applyErr != nil {
			return drained, service.heldLocked(), applyErr
		}
		if len(applied) != 0 {
			return drained, service.heldLocked(),
				fmt.Errorf("native surface shutdown inventory for %s is not empty: %d", name, len(applied))
		}
		drained += len(receipt.Surfaces)
		receipt = Receipt{Sequence: sequence, Accepted: true, Surfaces: []AppliedSurface{}, AppliedAtUnixMs: float64(time.Now().UnixNano()) / 1e6}
		service.latest[name] = receipt
		commit := windowCommit{declared: Snapshot{Window: name, Sequence: sequence}, applied: receipt}
		service.committed[name] = commit
		service.appendHistoryLocked(name, commit)
	}
	return drained, service.heldLocked(), nil
}

// heldLocked counts the surfaces still in every window's applied inventory. The
// caller holds the lock.
func (service *Service) heldLocked() int {
	held := 0
	for _, receipt := range service.latest {
		held += len(receipt.Surfaces)
	}
	return held
}

// Windows names every window this service has accepted a commit for, sorted.
//
// A reader that has to know the window names in advance cannot sweep, and a
// surface in a window nobody thought to ask about is the one that goes missing.
func (service *Service) Windows() []string {
	service.mu.Lock()
	defer service.mu.Unlock()
	names := make([]string, 0, len(service.latest))
	for name := range service.latest {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
