package nativesurface

import (
	"errors"
	"strings"
	"testing"
	"unsafe"
)

// errUnapplied is what a backend answers when an inventory did not land.
var errUnapplied = errors.New("browser native batch inventory mismatch: desired=1 applied=0")

// driftingBackend applies a declaration differently from how it was written.
//
// shift moves and resizes every surface it applies, drop names a declaration it
// never applies, extra is a surface it holds that no declaration asked for, and
// refuse turns the next apply into a refusal.
type driftingBackend struct {
	shift   Frame
	settled *Frame
	drop    string
	extra   *AppliedSurface
	refuse  error
}

func (backend *driftingBackend) Deliver(string, map[string]any) (map[string]any, error) {
	return nil, nil
}

func (backend *driftingBackend) Apply(window unsafe.Pointer, snapshot Snapshot) ([]AppliedSurface, error) {
	if backend.refuse != nil {
		return nil, backend.refuse
	}
	out := make([]AppliedSurface, 0, len(snapshot.Surfaces))
	for _, surface := range snapshot.Surfaces {
		if surface.ID == backend.drop {
			continue
		}
		out = append(out, AppliedSurface{
			ID:         surface.ID,
			Generation: surface.Generation,
			Frame: Frame{
				X:      surface.Frame.X + backend.shift.X,
				Y:      surface.Frame.Y + backend.shift.Y,
				Width:  surface.Frame.Width + backend.shift.Width,
				Height: surface.Frame.Height + backend.shift.Height,
			},
			Settled: backend.settled,
			Visible: surface.Visible,
			Alpha:   surface.Alpha,
			Layer:   surface.Layer,
			Window:  window,
		})
	}
	if backend.extra != nil {
		held := *backend.extra
		held.Window = window
		out = append(out, held)
	}
	return out, nil
}

func TestTheCompositionCarriesInteractivePresentationFacts(t *testing.T) {
	settled := Frame{X: 256, Y: 20, Width: 544, Height: 580}
	service := oneWindow(t, &driftingBackend{settled: &settled}, "browser")
	if _, err := service.Commit(Snapshot{Window: "win-a", Sequence: 8, Interactive: true, Surfaces: []Surface{{
		ID: "browser.win-a.tab-a", Generation: 1, Kind: "browser",
		Frame: Frame{X: 40, Y: 20, Width: 760, Height: 580}, Visible: true, Alpha: 1,
	}}}); err != nil {
		t.Fatalf("commit interactive inventory: %v", err)
	}
	composition := service.Latest("win-a")
	placement := placementOf(t, composition, "browser.win-a.tab-a")
	if !composition.Interactive || placement.Settled == nil || *placement.Settled != settled {
		t.Fatalf("interactive presentation facts were lost: %+v %+v", composition, placement)
	}
}

// oneWindow is a service with a single real window handle.
func oneWindow(t *testing.T, backend Backend, kinds ...SurfaceKind) *Service {
	t.Helper()
	// A real allocation, because the handle is compared against what the
	// backend reports and two nil handles compare equal.
	var handle byte
	return NewService(func(name string) unsafe.Pointer {
		if name != "win-a" {
			return nil
		}
		return unsafe.Pointer(&handle)
	}, wiredFor(backend, kinds...))
}

func placementOf(t *testing.T, composition Composition, id string) Placement {
	t.Helper()
	for _, placement := range composition.Surfaces {
		if placement.ID == id {
			return placement
		}
	}
	t.Fatalf("the composition holds no surface %s: %+v", id, composition.Surfaces)
	return Placement{}
}

// Both halves of one commit and the difference between them, from the half that
// holds both. A caller that has to subtract the two itself puts the arithmetic
// in every caller, and two callers reading one commit can then disagree.
func TestTheCompositionAnswersBothHalvesAndTheirDifference(t *testing.T) {
	service := oneWindow(t, &driftingBackend{shift: Frame{Y: 6}}, "browser")
	if _, err := service.Commit(Snapshot{Window: "win-a", Sequence: 7, Surfaces: []Surface{{
		ID: "browser.win-3ztbjd.tab-2trqyu", Generation: 3, Kind: "browser", Layer: 10,
		Frame: Frame{X: 10, Y: 20, Width: 300, Height: 200}, Visible: true, Alpha: 1,
	}}}); err != nil {
		t.Fatalf("commit: %v", err)
	}

	composition := service.Latest("win-a")
	if composition.Sequence != 7 {
		t.Errorf("the composition came from sequence %d, not the commit that produced it", composition.Sequence)
	}
	placement := placementOf(t, composition, "browser.win-3ztbjd.tab-2trqyu")
	if placement.Declared != (Frame{X: 10, Y: 20, Width: 300, Height: 200}) {
		t.Errorf("declared is %+v, not what the document asked for", placement.Declared)
	}
	if placement.Applied != (Frame{X: 10, Y: 26, Width: 300, Height: 200}) {
		t.Errorf("applied is %+v, not what the backend reported", placement.Applied)
	}
	if placement.Drift == nil {
		t.Fatal("a surface with both halves answers no difference")
	}
	if *placement.Drift != (Frame{Y: 6}) {
		t.Errorf("drift is %+v; applied minus declared is 6 points on y and nothing else", *placement.Drift)
	}
	if placement.Kind != "browser" || placement.Generation != 3 || placement.Layer != 10 {
		t.Errorf("the identity of the surface is %+v", placement)
	}
	if !placement.DeclaredVisible || placement.DeclaredAlpha != 1 {
		t.Errorf("the declared half lost visible or alpha: %+v", placement)
	}
	if !placement.AppliedVisible || placement.AppliedAlpha != 1 {
		t.Errorf("the applied half lost visible or alpha: %+v", placement)
	}
}

// Zero is the pass condition and it has to be reachable. A difference that can
// never be zero measures the arithmetic instead of the native layer.
func TestAnExactApplicationIsZeroDifference(t *testing.T) {
	service := oneWindow(t, &driftingBackend{}, "browser")
	if _, err := service.Commit(Snapshot{Window: "win-a", Sequence: 1, Surfaces: []Surface{{
		ID: "browser.win-3ztbjd.tab-2trqyu", Generation: 1, Kind: "browser",
		Frame: Frame{X: 4, Y: 8, Width: 100, Height: 50}, Visible: true, Alpha: 1,
	}}}); err != nil {
		t.Fatalf("commit: %v", err)
	}

	placement := placementOf(t, service.Latest("win-a"), "browser.win-3ztbjd.tab-2trqyu")
	if placement.Drift == nil || *placement.Drift != (Frame{}) {
		t.Errorf("drift is %v on a surface applied exactly where it was declared", placement.Drift)
	}
}

// A declared surface the backend never reported is named rather than dropped. A
// count that agreed while the screen did not is what this list prevents.
func TestADeclarationTheBackendNeverAppliedIsNamed(t *testing.T) {
	service := oneWindow(t, &driftingBackend{drop: "browser.win-3ztbjd.tab-qwdqt6"}, "browser")
	if _, err := service.Commit(Snapshot{Window: "win-a", Sequence: 2, Surfaces: []Surface{
		{
			ID: "browser.win-3ztbjd.tab-2trqyu", Generation: 1, Kind: "browser",
			Frame: Frame{Width: 10, Height: 10}, Visible: true, Alpha: 1,
		},
		{
			ID: "browser.win-3ztbjd.tab-qwdqt6", Generation: 1, Kind: "browser",
			Frame: Frame{Width: 10, Height: 10}, Visible: true, Alpha: 1,
		},
	}}); err != nil {
		t.Fatalf("commit: %v", err)
	}

	composition := service.Latest("win-a")
	if len(composition.Unapplied) != 1 || composition.Unapplied[0] != "browser.win-3ztbjd.tab-qwdqt6" {
		t.Errorf("unapplied is %v, not the one declaration the backend never applied", composition.Unapplied)
	}
	if len(composition.Surfaces) != 1 || composition.Surfaces[0].ID != "browser.win-3ztbjd.tab-2trqyu" {
		t.Errorf("the surfaces with both halves are %+v", composition.Surfaces)
	}
}

// A surface the native layer holds that no declaration asked for. It is the
// defect a ledger-only check cannot find: the application walks its own
// records, so a surface that left the records and stayed on screen is invisible
// to every check the application makes.
func TestASurfaceNoDeclarationAskedForIsNamed(t *testing.T) {
	service := oneWindow(t, &driftingBackend{extra: &AppliedSurface{
		ID: "browser.win-3ztbjd.tab-k6jivs", Generation: 1, Layer: 4,
		Frame: Frame{X: 5, Y: 5, Width: 20, Height: 20}, Visible: true, Alpha: 1,
	}}, "browser")
	if _, err := service.Commit(Snapshot{Window: "win-a", Sequence: 3, Surfaces: []Surface{{
		ID: "browser.win-3ztbjd.tab-2trqyu", Generation: 1, Kind: "browser",
		Frame: Frame{Width: 10, Height: 10}, Visible: true, Alpha: 1,
	}}}); err != nil {
		t.Fatalf("commit: %v", err)
	}

	uninvited := placementOf(t, service.Latest("win-a"), "browser.win-3ztbjd.tab-k6jivs")
	if !uninvited.Undeclared {
		t.Error("a surface no declaration asked for is reported as an ordinary placement")
	}
	if uninvited.Applied != (Frame{X: 5, Y: 5, Width: 20, Height: 20}) {
		t.Errorf("applied is %+v, not where the native layer reported it", uninvited.Applied)
	}
	if placementOf(t, service.Latest("win-a"), "browser.win-3ztbjd.tab-2trqyu").Undeclared {
		t.Error("a surface the document declared is reported undeclared")
	}
}

// A surface with one half has no difference. There is no second rectangle to
// subtract, so answering a number would answer one for something that has none,
// and answering zero would call a pane with no surface correct.
func TestASurfaceWithOneHalfHasNoDifference(t *testing.T) {
	service := oneWindow(t, &driftingBackend{extra: &AppliedSurface{
		ID: "browser.win-3ztbjd.tab-k6jivs", Generation: 1,
		Frame: Frame{X: 5, Y: 5, Width: 20, Height: 20}, Visible: true, Alpha: 1,
	}}, "browser")
	if _, err := service.Commit(Snapshot{Window: "win-a", Sequence: 1, Surfaces: []Surface{{
		ID: "browser.win-3ztbjd.tab-2trqyu", Generation: 1, Kind: "browser",
		Frame: Frame{Width: 10, Height: 10}, Visible: true, Alpha: 1,
	}}}); err != nil {
		t.Fatalf("commit: %v", err)
	}

	if drift := placementOf(t, service.Latest("win-a"), "browser.win-3ztbjd.tab-k6jivs").Drift; drift != nil {
		t.Errorf("drift is %+v on a surface with nothing to subtract from", *drift)
	}
}

// The window a surface landed in is carried into the composition, and it is not
// a distance. Every other number describes a rectangle inside some window, so
// all of them read correct while the rectangle is inside a window nobody is
// looking at.
func TestAMisparentedSurfaceIsCarriedIntoTheComposition(t *testing.T) {
	var elsewhere byte
	service := oneWindow(t, &misparentingBackend{putIn: unsafe.Pointer(&elsewhere)}, "browser")
	if _, err := service.Commit(Snapshot{Window: "win-a", Sequence: 1, Surfaces: []Surface{aSurface("browser.win-3ztbjd.tab-2trqyu")}}); err != nil {
		t.Fatalf("commit: %v", err)
	}

	placement := placementOf(t, service.Latest("win-a"), "browser.win-3ztbjd.tab-2trqyu")
	if !placement.Misparented {
		t.Error("a surface put in another window is reported as correct")
	}
	if placement.Drift == nil || *placement.Drift != (Frame{}) {
		t.Errorf("drift is %v; the rectangle matched and a window is not a distance", placement.Drift)
	}
}

// A backend that refuses every new inventory keeps the last one that landed as
// the composition, and every reading then reports a healthy layer. The refusal
// is the fact worth having.
func TestARefusedApplyIsCarriedIntoTheComposition(t *testing.T) {
	backend := &driftingBackend{}
	service := oneWindow(t, backend, "browser")
	declaration := Snapshot{Window: "win-a", Sequence: 4, Surfaces: []Surface{{
		ID: "browser.win-3ztbjd.tab-2trqyu", Generation: 1, Kind: "browser",
		Frame: Frame{Width: 10, Height: 10}, Visible: true, Alpha: 1,
	}}}
	if _, err := service.Commit(declaration); err != nil {
		t.Fatalf("commit: %v", err)
	}
	backend.refuse = errUnapplied
	declaration.Sequence = 5
	if _, err := service.Commit(declaration); err == nil {
		t.Fatal("the refused apply was answered as a commit")
	}

	composition := service.Latest("win-a")
	// What the backend said, and which kind's backend said it. With one backend per kind, a failure
	// that named neither would leave a reader unable to tell which half of a mixed inventory failed.
	if !strings.Contains(composition.Failure, errUnapplied.Error()) {
		t.Errorf("failure is %q, not what the backend said", composition.Failure)
	}
	if !strings.Contains(composition.Failure, `"browser"`) {
		t.Errorf("failure is %q and names no kind", composition.Failure)
	}
	if composition.FailedSequence != 5 {
		t.Errorf("the refusal happened at %d, not 5", composition.FailedSequence)
	}
	if composition.Sequence != 4 {
		t.Errorf("the numbers describe sequence %d; 4 is the last one that landed", composition.Sequence)
	}
}

// One window's composition is no answer about another's.
func TestTheCompositionIsOneWindowsOwn(t *testing.T) {
	var orchestrator, workspace byte
	handles := map[string]unsafe.Pointer{
		"main":  unsafe.Pointer(&orchestrator),
		"win-a": unsafe.Pointer(&workspace),
	}
	service := NewService(func(name string) unsafe.Pointer { return handles[name] }, wiredFor(&driftingBackend{}, "browser"))
	if _, err := service.Commit(Snapshot{Window: "win-a", Sequence: 1, Surfaces: []Surface{aSurface("browser.win-3ztbjd.tab-2trqyu")}}); err != nil {
		t.Fatalf("commit: %v", err)
	}

	if held := service.Latest("main"); len(held.Surfaces) != 0 || held.Sequence != 0 {
		t.Errorf("the orchestrator answers %+v; it declared nothing", held)
	}
	if held := service.Latest("win-a"); len(held.Surfaces) != 1 {
		t.Errorf("the declaring window answers %d surfaces, not 1", len(held.Surfaces))
	}
}

// A surface id is opaque to this service.
//
// The ids above are the shape the application issues, so the real value is what
// the tests run on. This one is deliberately none of it: no delimiter the
// application uses, no kind this service could recognise, and a length nothing
// here bounds.
//
// The service stores, pairs and answers by identity alone — it parses no field
// out of an id, and a compositor that did would need editing for every naming
// decision the application above it makes.
func TestASurfaceIdentityIsOpaqueToTheCompositor(t *testing.T) {
	const foreign = "a surface named by somebody else/entirely::42"

	service := oneWindow(t, &driftingBackend{}, "some-other-kind")
	if _, err := service.Commit(Snapshot{
		Window: "win-a", Sequence: 1,
		Surfaces: []Surface{{
			ID: foreign, Generation: 1, Kind: "some-other-kind",
			Frame: Frame{Width: 10, Height: 10}, Visible: true, Alpha: 1,
		}},
	}); err != nil {
		t.Fatalf("a foreign identity was refused: %v", err)
	}

	placement := placementOf(t, service.Latest("win-a"), foreign)
	if placement.ID != foreign {
		t.Errorf("the identity came back as %q, not the one committed", placement.ID)
	}
	if placement.Kind != "some-other-kind" {
		t.Errorf("the kind came back as %q; a kind is the application's word", placement.Kind)
	}
}
