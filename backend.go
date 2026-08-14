package nativesurface

import (
	"fmt"
	"sort"
	"sync"
	"unsafe"
)

type nativeAction uint8

const (
	nativeCreate nativeAction = iota + 1
	nativeUpdate
	nativeRemove
)

type nativeOwner struct {
	generation uint64
	native     unsafe.Pointer
	source     SurfaceSource
}

type nativeOperation struct {
	action   nativeAction
	surface  Surface
	native   unsafe.Pointer
	navigate bool
}

type nativeResult struct {
	surface Surface
	native  unsafe.Pointer
}

type nativeBatchDriver interface {
	apply(window unsafe.Pointer, operations []nativeOperation) ([]nativeResult, error)
}

type nativeBackend struct {
	mu     sync.Mutex
	driver nativeBatchDriver
	owners map[string]nativeOwner
}

func newNativeBackend(driver nativeBatchDriver) *nativeBackend {
	return &nativeBackend{driver: driver, owners: make(map[string]nativeOwner)}
}

// Apply is the only native writer. It plans the complete desired inventory,
// crosses the platform boundary once, then publishes owners only after the
// whole native batch succeeds.
func (backend *nativeBackend) Apply(window unsafe.Pointer, snapshot Snapshot) ([]AppliedSurface, error) {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	if backend.driver == nil {
		return nil, fmt.Errorf("native surface batch driver is not configured")
	}
	operations := planNativeBatch(backend.owners, snapshot)
	results, err := backend.driver.apply(window, operations)
	if err != nil {
		return nil, err
	}
	if len(results) != len(snapshot.Surfaces) {
		return nil, fmt.Errorf("native surface batch receipt inventory mismatch: desired=%d applied=%d", len(snapshot.Surfaces), len(results))
	}

	next := make(map[string]nativeOwner, len(results))
	applied := make([]AppliedSurface, 0, len(results))
	for _, result := range results {
		surface := result.surface
		if result.native == nil {
			return nil, fmt.Errorf("native surface batch returned empty owner: %s", surface.ID)
		}
		next[surface.ID] = nativeOwner{generation: surface.Generation, native: result.native, source: surface.Source}
		applied = append(applied, AppliedSurface{
			ID: surface.ID, Generation: surface.Generation, Frame: surface.Frame,
			Visible: surface.Visible, Alpha: surface.Alpha, Layer: surface.Layer,
		})
	}
	backend.owners = next
	return applied, nil
}

func planNativeBatch(current map[string]nativeOwner, snapshot Snapshot) []nativeOperation {
	desired := append([]Surface(nil), snapshot.Surfaces...)
	sort.Slice(desired, func(i, j int) bool {
		if desired[i].Layer != desired[j].Layer {
			return desired[i].Layer < desired[j].Layer
		}
		return desired[i].ID < desired[j].ID
	})
	seen := make(map[string]struct{}, len(desired))
	operations := make([]nativeOperation, 0, len(current)+len(desired))
	for _, surface := range desired {
		seen[surface.ID] = struct{}{}
		owner, exists := current[surface.ID]
		if !exists {
			operations = append(operations, nativeOperation{action: nativeCreate, surface: surface})
			continue
		}
		if owner.generation != surface.Generation {
			operations = append(operations,
				nativeOperation{action: nativeRemove, surface: Surface{ID: surface.ID, Generation: owner.generation}, native: owner.native},
				nativeOperation{action: nativeCreate, surface: surface},
			)
			continue
		}
		operations = append(operations, nativeOperation{
			action: nativeUpdate, surface: surface, native: owner.native,
			navigate: owner.source != surface.Source,
		})
	}

	removed := make([]string, 0)
	for id := range current {
		if _, exists := seen[id]; !exists {
			removed = append(removed, id)
		}
	}
	sort.Strings(removed)
	for _, id := range removed {
		owner := current[id]
		operations = append(operations, nativeOperation{
			action:  nativeRemove,
			surface: Surface{ID: id, Generation: owner.generation},
			native:  owner.native,
		})
	}
	return operations
}
