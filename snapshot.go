package nativesurface

import (
	"fmt"
	"math"
	"sort"
	"sync"
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
	Window   string    `json:"window"`
	Sequence uint64    `json:"sequence"`
	Surfaces []Surface `json:"surfaces"`
}

type AppliedSurface struct {
	ID         string  `json:"id"`
	Generation uint64  `json:"generation"`
	Frame      Frame   `json:"frame"`
	Visible    bool    `json:"visible"`
	Alpha      float64 `json:"alpha"`
	Layer      int     `json:"layer"`
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
	windows   func(name string) unsafe.Pointer
	backend   Backend
	latest    map[string]Receipt
	committed map[string]windowCommit
	stopped   bool
}

func NewService(windows func(name string) unsafe.Pointer, backend Backend) *Service {
	return &Service{
		windows:   windows,
		backend:   backend,
		latest:    map[string]Receipt{},
		committed: map[string]windowCommit{},
	}
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
	if service.backend == nil {
		return Receipt{}, fmt.Errorf("native surface backend is not configured")
	}
	applied, err := service.backend.Apply(handle, snapshot)
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
	receipt := Receipt{Sequence: snapshot.Sequence, Accepted: true, Surfaces: applied}
	service.latest[snapshot.Window] = receipt
	service.committed[snapshot.Window] = windowCommit{declared: snapshot, applied: receipt}
	return receipt, nil
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
	if service.backend == nil {
		return nil, fmt.Errorf("native surface %s cannot be driven: there is no backend", id)
	}
	// Every window's inventory. A surface id is unique across the application,
	// so the window it is in is not something a caller has to know to drive it.
	for _, receipt := range service.latest {
		for _, surface := range receipt.Surfaces {
			if surface.ID == id {
				return service.backend.Deliver(id, message)
			}
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
		if service.backend == nil {
			return drained, service.heldLocked(),
				fmt.Errorf("native surface backend is unavailable during shutdown")
		}
		sequence := receipt.Sequence + 1
		applied, applyErr := service.backend.Apply(handle, Snapshot{Window: name, Sequence: sequence})
		if applyErr != nil {
			return drained, service.heldLocked(), applyErr
		}
		if len(applied) != 0 {
			return drained, service.heldLocked(),
				fmt.Errorf("native surface shutdown inventory for %s is not empty: %d", name, len(applied))
		}
		drained += len(receipt.Surfaces)
		service.latest[name] = Receipt{Sequence: sequence, Accepted: true, Surfaces: []AppliedSurface{}}
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
