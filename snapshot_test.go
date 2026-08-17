package nativesurface

import (
	"testing"
	"unsafe"
)

func TestCommitAppliesOneValidatedInventoryAndRejectsStaleSnapshots(t *testing.T) {
	backend := &recordingBackend{}
	window := byte(1)
	service := NewService(func(string) unsafe.Pointer { return unsafe.Pointer(&window) }, backend)
	first := Snapshot{
		Window:   "win-a",
		Sequence: 1,
		Surfaces: []Surface{
			{ID: "surface-left", Generation: 1, Kind: SurfaceKind("test-surface"), Frame: Frame{X: 0, Y: 0, Width: 400, Height: 600}, Visible: true, Source: SurfaceSource{"owner": "left"}},
			{ID: "surface-right", Generation: 1, Kind: SurfaceKind("test-surface"), Frame: Frame{X: 400, Y: 0, Width: 400, Height: 600}, Visible: true, Source: SurfaceSource{"owner": "right"}},
		},
	}

	receipt, err := service.Commit(first)
	if err != nil {
		t.Fatalf("commit inventory: %v", err)
	}
	if !receipt.Accepted || receipt.Sequence != 1 || len(receipt.Surfaces) != 2 {
		t.Fatalf("commit must return the whole applied inventory: %+v", receipt)
	}
	if len(backend.snapshots) != 1 || backend.snapshots[0].Sequence != 1 {
		t.Fatalf("backend must receive one atomic snapshot: %+v", backend.snapshots)
	}

	stale, err := service.Commit(first)
	if err != nil {
		t.Fatalf("stale snapshot is a structured rejection, not transport failure: %v", err)
	}
	if stale.Accepted || len(backend.snapshots) != 1 {
		t.Fatalf("stale snapshot must not reach the native writer: %+v", stale)
	}
}

func TestSnapshotRejectsDuplicateSurfaceOwners(t *testing.T) {
	window := byte(1)
	service := NewService(func(string) unsafe.Pointer { return unsafe.Pointer(&window) }, &recordingBackend{})
	_, err := service.Commit(Snapshot{Window: "win-a", Sequence: 1, Surfaces: []Surface{
		{ID: "same", Generation: 1, Kind: SurfaceKind("test-surface"), Frame: Frame{Width: 10, Height: 10}},
		{ID: "same", Generation: 2, Kind: SurfaceKind("test-surface"), Frame: Frame{Width: 10, Height: 10}},
	}})
	if err == nil {
		t.Fatal("one snapshot cannot contain duplicate surface owners")
	}
}

func TestCompositorDelegatesOpaqueSurfaceKindsToItsBackend(t *testing.T) {
	backend := &recordingBackend{}
	window := byte(1)
	service := NewService(func(string) unsafe.Pointer { return unsafe.Pointer(&window) }, backend)
	_, err := service.Commit(Snapshot{Window: "win-a", Sequence: 1, Surfaces: []Surface{{
		ID: "project-defined", Generation: 1, Kind: SurfaceKind("project-native-kind"),
		Frame: Frame{Width: 10, Height: 10}, Visible: true, Alpha: 1,
	}}})
	if err != nil {
		t.Fatalf("compositor must not know backend-owned surface kinds: %v", err)
	}
}

func TestServiceShutdownAppliesOneEmptyInventory(t *testing.T) {
	backend := &recordingBackend{}
	window := byte(1)
	service := NewService(func(string) unsafe.Pointer { return unsafe.Pointer(&window) }, backend)
	if _, err := service.Commit(Snapshot{Window: "win-a", Sequence: 1, Surfaces: []Surface{{
		ID: "surface-1", Generation: 1, Kind: SurfaceKind("test-surface"),
		Frame: Frame{Width: 10, Height: 10}, Visible: true, Alpha: 1,
	}}}); err != nil {
		t.Fatalf("commit surface inventory: %v", err)
	}
	if err := service.ServiceShutdown(); err != nil {
		t.Fatalf("shutdown compositor service: %v", err)
	}
	if len(backend.snapshots) != 2 || len(backend.snapshots[1].Surfaces) != 0 {
		t.Fatalf("shutdown must remove every native surface in one empty inventory: %+v", backend.snapshots)
	}
	status := service.Status("win-a")
	if !status.Accepted || status.Sequence != 2 || len(status.Surfaces) != 0 {
		t.Fatalf("shutdown status must expose the applied empty inventory: %+v", status)
	}
}

type recordingBackend struct {
	snapshots []Snapshot
}

func (backend *recordingBackend) Deliver(string, map[string]any) (map[string]any, error) {
	return nil, nil
}

func (backend *recordingBackend) Apply(_ unsafe.Pointer, snapshot Snapshot) ([]AppliedSurface, error) {
	backend.snapshots = append(backend.snapshots, snapshot)
	result := make([]AppliedSurface, len(snapshot.Surfaces))
	for index, surface := range snapshot.Surfaces {
		result[index] = AppliedSurface{ID: surface.ID, Generation: surface.Generation, Frame: surface.Frame, Visible: surface.Visible, Alpha: surface.Alpha, Layer: surface.Layer}
	}
	return result, nil
}
