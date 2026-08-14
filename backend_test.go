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
