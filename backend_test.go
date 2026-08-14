package nativesurface

import "testing"

func TestPlanNativeBatchPreservesGenerationAndRemovesAbsentOwners(t *testing.T) {
	current := map[string]nativeOwner{
		"left":  {generation: 1, native: 11},
		"stale": {generation: 1, native: 12},
	}
	snapshot := Snapshot{Sequence: 2, Surfaces: []Surface{
		{ID: "left", Generation: 1, Kind: BrowserSurface, Frame: Frame{Width: 300, Height: 600}, Visible: true, Alpha: 1, Layer: 10},
		{ID: "right", Generation: 1, Kind: BrowserSurface, Frame: Frame{X: 300, Width: 500, Height: 600}, Visible: true, Alpha: 1, Layer: 20},
	}}

	operations := planNativeBatch(current, snapshot)
	if len(operations) != 3 {
		t.Fatalf("one batch must update, create, and remove exact inventory: %+v", operations)
	}
	if operations[0].action != nativeUpdate || operations[0].surface.ID != "left" || operations[0].native != 11 {
		t.Fatalf("same generation must preserve its native owner: %+v", operations[0])
	}
	if operations[1].action != nativeCreate || operations[1].surface.ID != "right" {
		t.Fatalf("new identity must be created at its declared layer: %+v", operations[1])
	}
	if operations[2].action != nativeRemove || operations[2].surface.ID != "stale" || operations[2].native != 12 {
		t.Fatalf("absent identity must be removed in the same batch: %+v", operations[2])
	}
}

type recordingNativeBatchDriver struct {
	calls      int
	operations []nativeOperation
	next       uintptr
}

func (driver *recordingNativeBatchDriver) apply(_ uintptr, operations []nativeOperation) ([]nativeResult, error) {
	driver.calls++
	driver.operations = append([]nativeOperation(nil), operations...)
	results := make([]nativeResult, 0, len(operations))
	for _, operation := range operations {
		native := operation.native
		if operation.action == nativeCreate {
			driver.next++
			native = driver.next
		}
		if operation.action != nativeRemove {
			results = append(results, nativeResult{surface: operation.surface, native: native})
		}
	}
	return results, nil
}

func TestNativeBackendAppliesEachSnapshotAsOneBatchAndCommitsOwnersAfterSuccess(t *testing.T) {
	driver := &recordingNativeBatchDriver{next: 40}
	backend := newNativeBackend(driver)
	first := Snapshot{Sequence: 1, Surfaces: []Surface{
		{ID: "left", Generation: 1, Kind: BrowserSurface, Frame: Frame{Width: 300, Height: 600}, Visible: true, Alpha: 1, Layer: 10},
		{ID: "right", Generation: 1, Kind: BrowserSurface, Frame: Frame{X: 300, Width: 500, Height: 600}, Visible: true, Alpha: 1, Layer: 20},
	}}

	applied, err := backend.Apply(99, first)
	if err != nil {
		t.Fatalf("apply first native inventory: %v", err)
	}
	if driver.calls != 1 || len(driver.operations) != 2 || len(applied) != 2 {
		t.Fatalf("one snapshot must cross the native boundary exactly once: calls=%d operations=%+v applied=%+v", driver.calls, driver.operations, applied)
	}
	leftNative := backend.owners["left"].native
	if leftNative == 0 || backend.owners["right"].native == 0 {
		t.Fatalf("successful batch must commit exact native owners: %+v", backend.owners)
	}

	second := Snapshot{Sequence: 2, Surfaces: []Surface{
		{ID: "left", Generation: 1, Kind: BrowserSurface, Frame: Frame{Width: 800, Height: 600}, Visible: true, Alpha: 1, Layer: 10},
	}}
	applied, err = backend.Apply(99, second)
	if err != nil {
		t.Fatalf("apply replacement inventory: %v", err)
	}
	if driver.calls != 2 || len(driver.operations) != 2 || driver.operations[0].action != nativeUpdate || driver.operations[1].action != nativeRemove {
		t.Fatalf("update and removal must share one second native batch: %+v", driver.operations)
	}
	if backend.owners["left"].native != leftNative || len(backend.owners) != 1 || len(applied) != 1 {
		t.Fatalf("same generation must preserve owner and absent surfaces must be removed: owners=%+v applied=%+v", backend.owners, applied)
	}
}
