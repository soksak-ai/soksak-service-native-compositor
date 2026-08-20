package nativesurface

import (
	"errors"
	"testing"
	"unsafe"
)

var errRefusedDrain = errors.New("this backend will not empty a window")

// Shutdown reports what it drained, and what is left.
//
// A host quitting answers a receipt a caller checks before the process goes
// away: how many surfaces were taken down, and whether any remain. A drain that
// worked and reported nothing cannot be part of that receipt.
//
// Remaining is the number that matters. A surface still held when the process
// exits is a native child outliving its parent, and the two facts — "I took
// down four" and "none are left" — are not the same claim.
func TestDrainAnswersWhatItTookDownAndWhatIsLeft(t *testing.T) {
	handle := byte(1)
	backend := &drainRecorder{}
	service := NewService(func(string) unsafe.Pointer { return unsafe.Pointer(&handle) }, wiredFor(backend, "browser"))

	for _, window := range []string{"win-3ztbjd", "win-9m3xb5"} {
		if _, err := service.Commit(Snapshot{
			Window: window, Sequence: 1,
			Surfaces: []Surface{
				{ID: window + ".a", Generation: 1, Kind: "browser", Frame: Frame{Width: 10, Height: 10}, Visible: true, Alpha: 1},
				{ID: window + ".b", Generation: 1, Kind: "browser", Frame: Frame{Width: 10, Height: 10}, Visible: true, Alpha: 1},
			},
		}); err != nil {
			t.Fatalf("commit in %s: %v", window, err)
		}
	}

	drained, remaining, err := service.Drain()
	if err != nil {
		t.Fatalf("drain: %v", err)
	}
	if drained != 4 {
		t.Errorf("Drain took down %d surfaces, want 4", drained)
	}
	if remaining != 0 {
		t.Errorf("%d surfaces are left after a drain", remaining)
	}

	// Idempotent, and the second answer is zero: a count that repeated itself
	// would report work that did not happen.
	drained, remaining, err = service.Drain()
	if err != nil {
		t.Fatalf("second drain: %v", err)
	}
	if drained != 0 || remaining != 0 {
		t.Errorf("a second drain answered %d taken down and %d left, want 0 and 0", drained, remaining)
	}
}

// A backend that will not empty a window leaves surfaces behind, and the number
// says so rather than the drain reporting success.
func TestADrainThatCouldNotEmptyAWindowSaysHowManyAreLeft(t *testing.T) {
	handle := byte(1)
	service := NewService(
		func(string) unsafe.Pointer { return unsafe.Pointer(&handle) },
		wiredFor(&drainRecorder{refuse: true}, "browser"))

	if _, err := service.Commit(Snapshot{
		Window: "win-3ztbjd", Sequence: 1,
		Surfaces: []Surface{{
			ID: "browser.win-3ztbjd.tab-2trqyu", Generation: 1, Kind: "browser",
			Frame: Frame{Width: 10, Height: 10}, Visible: true, Alpha: 1,
		}},
	}); err != nil {
		t.Fatalf("commit: %v", err)
	}

	drained, remaining, err := service.Drain()
	if err == nil {
		t.Fatal("a backend that refused to empty a window was reported as a clean drain")
	}
	if drained != 0 {
		t.Errorf("nothing came down and the drain counted %d", drained)
	}
	if remaining != 1 {
		t.Errorf("one surface is still held and remaining is %d", remaining)
	}
}

type drainRecorder struct{ refuse bool }

func (backend *drainRecorder) Apply(window unsafe.Pointer, snapshot Snapshot) ([]AppliedSurface, error) {
	if backend.refuse && len(snapshot.Surfaces) == 0 {
		return nil, errRefusedDrain
	}
	applied := make([]AppliedSurface, 0, len(snapshot.Surfaces))
	for _, surface := range snapshot.Surfaces {
		applied = append(applied, AppliedSurface{
			ID: surface.ID, Generation: surface.Generation, Frame: surface.Frame,
			Visible: surface.Visible, Alpha: surface.Alpha, Layer: surface.Layer, Window: window,
		})
	}
	return applied, nil
}

func (backend *drainRecorder) Deliver(string, map[string]any) (map[string]any, error) {
	return nil, nil
}
