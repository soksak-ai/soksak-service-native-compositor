package nativesurface

import (
	"testing"
	"unsafe"
)

func TestCommitAppliesOneValidatedInventoryAndRejectsStaleSnapshots(t *testing.T) {
	backend := &recordingBackend{}
	window := byte(1)
	service := NewService(func() unsafe.Pointer { return unsafe.Pointer(&window) }, backend)
	first := Snapshot{
		Sequence: 1,
		Surfaces: []Surface{
			{ID: "browser-left", Generation: 1, Kind: BrowserSurface, Frame: Frame{X: 0, Y: 0, Width: 400, Height: 600}, Visible: true, Source: SurfaceSource{"url": "https://example.com/left"}},
			{ID: "browser-right", Generation: 1, Kind: BrowserSurface, Frame: Frame{X: 400, Y: 0, Width: 400, Height: 600}, Visible: true, Source: SurfaceSource{"url": "https://example.com/right"}},
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
	service := NewService(func() unsafe.Pointer { return unsafe.Pointer(&window) }, &recordingBackend{})
	_, err := service.Commit(Snapshot{Sequence: 1, Surfaces: []Surface{
		{ID: "same", Generation: 1, Kind: BrowserSurface, Frame: Frame{Width: 10, Height: 10}},
		{ID: "same", Generation: 2, Kind: BrowserSurface, Frame: Frame{Width: 10, Height: 10}},
	}})
	if err == nil {
		t.Fatal("one snapshot cannot contain duplicate surface owners")
	}
}

type recordingBackend struct {
	snapshots []Snapshot
}

func (backend *recordingBackend) Apply(_ unsafe.Pointer, snapshot Snapshot) ([]AppliedSurface, error) {
	backend.snapshots = append(backend.snapshots, snapshot)
	result := make([]AppliedSurface, len(snapshot.Surfaces))
	for index, surface := range snapshot.Surfaces {
		result[index] = AppliedSurface{ID: surface.ID, Generation: surface.Generation, Frame: surface.Frame, Visible: surface.Visible, Alpha: surface.Alpha, Layer: surface.Layer}
	}
	return result, nil
}
