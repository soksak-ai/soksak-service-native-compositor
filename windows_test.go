package nativesurface

import (
	"testing"
	"unsafe"
)

// A surface lands in the window whose document declared it.
//
// Measured 2026-08-16 in soksak: this service held one window accessor, and the host it runs in
// passed the first window created — its orchestrator. Every workspace window's browser was then
// attached to the orchestrator, at coordinates a 1300x900 document had computed, inside a 999x617
// window. The pane where the page belonged showed the window's background and the page was on
// screen nowhere. Nothing refused, nothing drifted, and every reading said the layer was healthy:
// the compositor was asked which window, could not be asked, and answered anyway.
type windowRecordingBackend struct {
	applied map[unsafe.Pointer][]string
}

func (backend *windowRecordingBackend) Deliver(string, map[string]any) (map[string]any, error) {
	return nil, nil
}

func (backend *windowRecordingBackend) Apply(window unsafe.Pointer, snapshot Snapshot) ([]AppliedSurface, error) {
	if backend.applied == nil {
		backend.applied = map[unsafe.Pointer][]string{}
	}
	var out []AppliedSurface
	for _, surface := range snapshot.Surfaces {
		backend.applied[window] = append(backend.applied[window], surface.ID)
		out = append(out, AppliedSurface{
			ID: surface.ID, Generation: surface.Generation, Frame: surface.Frame,
			Visible: surface.Visible, Alpha: surface.Alpha, Layer: surface.Layer,
		})
	}
	return out, nil
}

func aSurface(id string) Surface {
	return Surface{
		ID: id, Generation: 1, Kind: "browser", Alpha: 1, Visible: true,
		Frame: Frame{X: 10, Y: 10, Width: 100, Height: 100},
	}
}

func TestASurfaceLandsInTheWindowThatDeclaredIt(t *testing.T) {
	var orchestrator, workspace byte
	handles := map[string]unsafe.Pointer{
		"main":  unsafe.Pointer(&orchestrator),
		"win-a": unsafe.Pointer(&workspace),
	}
	backend := &windowRecordingBackend{}
	service := NewService(func(name string) unsafe.Pointer { return handles[name] }, wiredFor(backend, "browser"))

	if _, err := service.Commit(Snapshot{Window: "win-a", Sequence: 1, Surfaces: []Surface{aSurface("brw-1")}}); err != nil {
		t.Fatalf("the workspace window's commit was refused: %v", err)
	}
	if got := backend.applied[handles["main"]]; len(got) != 0 {
		t.Errorf("the orchestrator received %v; it declared nothing", got)
	}
	if got := backend.applied[handles["win-a"]]; len(got) != 1 || got[0] != "brw-1" {
		t.Errorf("the declaring window received %v, not [brw-1]", got)
	}
}

// Each window counts its own sequence. One counter across all of them makes the
// second window's first commit arrive behind the first window's tenth, and a
// commit behind the counter is answered as stale — so that window never gets a
// surface and is told nothing is wrong.
func TestOneWindowsSequenceDoesNotStaleAnother(t *testing.T) {
	var orchestrator, workspace byte
	handles := map[string]unsafe.Pointer{
		"main":  unsafe.Pointer(&orchestrator),
		"win-a": unsafe.Pointer(&workspace),
	}
	backend := &windowRecordingBackend{}
	service := NewService(func(name string) unsafe.Pointer { return handles[name] }, wiredFor(backend, "browser"))

	for sequence := uint64(1); sequence <= 10; sequence++ {
		if _, err := service.Commit(Snapshot{Window: "main", Sequence: sequence, Surfaces: []Surface{aSurface("srf-main")}}); err != nil {
			t.Fatalf("the orchestrator's commit %d was refused: %v", sequence, err)
		}
	}
	receipt, err := service.Commit(Snapshot{Window: "win-a", Sequence: 1, Surfaces: []Surface{aSurface("brw-1")}})
	if err != nil {
		t.Fatalf("the workspace window's first commit was refused: %v", err)
	}
	if !receipt.Accepted {
		t.Fatalf("the workspace window's first commit was answered stale at sequence %d", receipt.Sequence)
	}
	if got := backend.applied[handles["win-a"]]; len(got) != 1 {
		t.Errorf("the workspace window received %v, not one surface", got)
	}
}

// A reading names its window. Answering the whole application's inventory to a
// question about one window is how a capture drew another window's page into
// this one.
func TestAReadingAnswersForOneWindow(t *testing.T) {
	var orchestrator, workspace byte
	handles := map[string]unsafe.Pointer{
		"main":  unsafe.Pointer(&orchestrator),
		"win-a": unsafe.Pointer(&workspace),
	}
	service := NewService(func(name string) unsafe.Pointer { return handles[name] }, wiredFor(&windowRecordingBackend{}, "browser"))

	if _, err := service.Commit(Snapshot{Window: "win-a", Sequence: 1, Surfaces: []Surface{aSurface("brw-1")}}); err != nil {
		t.Fatalf("commit: %v", err)
	}
	if held := service.Status("main").Surfaces; len(held) != 0 {
		t.Errorf("the orchestrator answers %d surfaces; it declared none", len(held))
	}
	if held := service.Status("win-a").Surfaces; len(held) != 1 {
		t.Errorf("the declaring window answers %d surfaces, not 1", len(held))
	}
	if composed := service.Latest("main").Surfaces; len(composed) != 0 {
		t.Errorf("the orchestrator's last commit holds %d surfaces; it made none", len(composed))
	}
}

// The backend reports which window each surface actually landed in, and the
// compositor compares that with the window it handed over.
//
// Until this read-back existed, "the surface is in the wrong window" could only
// be argued from source. Every number the compositor published — declared
// frame, applied frame, drift, visible, alpha — is a fact about a rectangle
// inside some window, and all of them read correct while the rectangle is
// inside a window nobody is looking at. The window is the one coordinate that
// was never read back.
type misparentingBackend struct {
	putIn unsafe.Pointer
}

func (backend *misparentingBackend) Deliver(string, map[string]any) (map[string]any, error) {
	return nil, nil
}

func (backend *misparentingBackend) Apply(_ unsafe.Pointer, snapshot Snapshot) ([]AppliedSurface, error) {
	var out []AppliedSurface
	for _, surface := range snapshot.Surfaces {
		out = append(out, AppliedSurface{
			ID: surface.ID, Generation: surface.Generation, Frame: surface.Frame,
			Visible: surface.Visible, Alpha: surface.Alpha, Layer: surface.Layer,
			Window: backend.putIn,
		})
	}
	return out, nil
}

func TestASurfaceInAnotherWindowIsReported(t *testing.T) {
	var orchestrator, workspace byte
	handles := map[string]unsafe.Pointer{
		"main":  unsafe.Pointer(&orchestrator),
		"win-a": unsafe.Pointer(&workspace),
	}
	backend := &misparentingBackend{putIn: handles["main"]}
	service := NewService(func(name string) unsafe.Pointer { return handles[name] }, wiredFor(backend, "browser"))

	receipt, err := service.Commit(Snapshot{Window: "win-a", Sequence: 1, Surfaces: []Surface{aSurface("brw-1")}})
	if err != nil {
		t.Fatalf("commit: %v", err)
	}
	if len(receipt.Surfaces) != 1 {
		t.Fatalf("the commit applied %d surfaces, not 1", len(receipt.Surfaces))
	}
	if !receipt.Surfaces[0].Misparented {
		t.Error("a surface put in the orchestrator while win-a declared it was reported as correct")
	}
}

func TestASurfaceInItsOwnWindowIsNotReportedMisparented(t *testing.T) {
	var workspace byte
	handles := map[string]unsafe.Pointer{"win-a": unsafe.Pointer(&workspace)}
	backend := &misparentingBackend{putIn: handles["win-a"]}
	service := NewService(func(name string) unsafe.Pointer { return handles[name] }, wiredFor(backend, "browser"))

	receipt, err := service.Commit(Snapshot{Window: "win-a", Sequence: 1, Surfaces: []Surface{aSurface("brw-1")}})
	if err != nil {
		t.Fatalf("commit: %v", err)
	}
	if receipt.Surfaces[0].Misparented {
		t.Error("a surface in the window that declared it was reported misparented")
	}
}

// A backend that reports no window is reported misparented rather than assumed
// correct. Silence is what a backend that has not been taught to read the
// window back produces, and reading that as agreement is how the defect stayed
// invisible.
func TestABackendThatReportsNoWindowIsNotTakenAtItsWord(t *testing.T) {
	var workspace byte
	handles := map[string]unsafe.Pointer{"win-a": unsafe.Pointer(&workspace)}
	service := NewService(func(name string) unsafe.Pointer { return handles[name] }, wiredFor(&misparentingBackend{putIn: nil}, "browser"))

	receipt, err := service.Commit(Snapshot{Window: "win-a", Sequence: 1, Surfaces: []Surface{aSurface("brw-1")}})
	if err != nil {
		t.Fatalf("commit: %v", err)
	}
	if !receipt.Surfaces[0].Misparented {
		t.Error("a backend that named no window was taken as agreeing")
	}
}
