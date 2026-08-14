package nativesurface

import "sort"

type nativeAction uint8

const (
	nativeCreate nativeAction = iota + 1
	nativeUpdate
	nativeRemove
)

type nativeOwner struct {
	generation uint64
	native     uintptr
	source     SurfaceSource
}

type nativeOperation struct {
	action  nativeAction
	surface Surface
	native  uintptr
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
		operations = append(operations, nativeOperation{action: nativeUpdate, surface: surface, native: owner.native})
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
