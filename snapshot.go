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
}

type Receipt struct {
	Sequence uint64           `json:"sequence"`
	Accepted bool             `json:"accepted"`
	Surfaces []AppliedSurface `json:"surfaces"`
}

type Backend interface {
	Apply(window unsafe.Pointer, snapshot Snapshot) ([]AppliedSurface, error)
}

type Service struct {
	mu        sync.Mutex
	window    func() unsafe.Pointer
	backend   Backend
	latest    Receipt
	committed Committed
	stopped   bool
}

func NewService(window func() unsafe.Pointer, backend Backend) *Service {
	return &Service{window: window, backend: backend}
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
	if snapshot.Sequence <= service.latest.Sequence {
		stale := service.latest
		stale.Accepted = false
		return stale, nil
	}
	if service.window == nil || service.window() == nil {
		return Receipt{}, fmt.Errorf("native surface window is not ready")
	}
	if service.backend == nil {
		return Receipt{}, fmt.Errorf("native surface backend is not configured")
	}
	applied, err := service.backend.Apply(service.window(), snapshot)
	if err != nil {
		// A refused attempt is recorded rather than forgotten. Without it the
		// last healthy inventory keeps answering, and every reading reports a
		// healthy native layer.
		service.committed.Failure = err.Error()
		service.committed.FailedSequence = snapshot.Sequence
		return Receipt{}, err
	}
	sort.Slice(applied, func(i, j int) bool { return applied[i].ID < applied[j].ID })
	service.latest = Receipt{Sequence: snapshot.Sequence, Accepted: true, Surfaces: applied}
	service.committed = Committed{Declared: snapshot, Applied: service.latest}
	return service.latest, nil
}

func (service *Service) Status() Receipt {
	service.mu.Lock()
	defer service.mu.Unlock()
	return service.latest
}

func (service *Service) ServiceShutdown() error {
	service.mu.Lock()
	defer service.mu.Unlock()
	if service.stopped {
		return nil
	}
	service.stopped = true
	if service.window == nil || service.window() == nil {
		if len(service.latest.Surfaces) == 0 {
			return nil
		}
		return fmt.Errorf("native surface window is unavailable during shutdown")
	}
	if service.backend == nil {
		if len(service.latest.Surfaces) == 0 {
			return nil
		}
		return fmt.Errorf("native surface backend is unavailable during shutdown")
	}
	sequence := service.latest.Sequence + 1
	applied, err := service.backend.Apply(service.window(), Snapshot{Sequence: sequence})
	if err != nil {
		return err
	}
	if len(applied) != 0 {
		return fmt.Errorf("native surface shutdown inventory is not empty: %d", len(applied))
	}
	service.latest = Receipt{Sequence: sequence, Accepted: true, Surfaces: []AppliedSurface{}}
	return nil
}

// Declared is the inventory the document asked for on the last accepted commit,
// and Applied is what the native layer reported back.
//
// Both halves come from one commit. A consumer that measured drift against the
// applied half alone would compare a value with itself and read zero every
// time; a consumer that read the declared half from the document would be
// reading a later frame than the one that was applied.
type Committed struct {
	Declared Snapshot `json:"declared"`
	Applied  Receipt  `json:"applied"`
	// Failure is the backend's reason for the most recent attempt that did not
	// land, empty when the last attempt landed. Without it a backend that
	// refuses every new inventory keeps answering with the last healthy one,
	// and every reading reports a healthy layer.
	Failure string `json:"failure"`
	// FailedSequence is the sequence of that attempt.
	FailedSequence uint64 `json:"failedSequence"`
}

// Latest answers both halves of the last commit.
func (service *Service) Latest() Committed {
	service.mu.Lock()
	defer service.mu.Unlock()
	return service.committed
}
