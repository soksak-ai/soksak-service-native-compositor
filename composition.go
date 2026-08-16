package nativesurface

// The compositing verdict: what one window's last commit declared, what the
// native layer reported back, and the difference between them.
//
// Both halves come from that one commit, which is what makes the difference a
// subtraction. A reader that measured against the applied half alone would
// subtract a value from itself and read zero every time; a reader that took the
// declared half from the document would compare a later frame against an
// earlier application, and the difference would land on the native layer, which
// did not produce it.
//
// This service holds both halves, so the subtraction is here. Taken by each
// caller instead, the rule is in every caller and two of them reading one
// commit can disagree about it.

// windowCommit is what one window's last commit recorded.
type windowCommit struct {
	declared Snapshot
	applied  Receipt
	// failure is the backend's reason for the most recent attempt that did not
	// land, empty when the last attempt landed. Without it a backend refusing
	// every new inventory keeps answering with the last healthy one, and every
	// reading reports a healthy layer.
	failure string
	// failedSequence is the sequence of that attempt.
	failedSequence uint64
}

// Composition is one window's last commit read as a comparison.
type Composition struct {
	// Sequence is the commit these numbers came from. Zero means no inventory
	// has ever been applied to this window, which is a different answer from an
	// inventory that was applied and held nothing: one is a compositor that has
	// never run, the other is a window whose document declares no surface.
	Sequence uint64 `json:"sequence"`
	// Surfaces is one entry per surface either half holds.
	Surfaces []Placement `json:"surfaces"`
	// Unapplied names the surfaces the document declared and the backend did
	// not report back. A count that agreed while the screen did not is what
	// this list prevents.
	Unapplied []string `json:"unapplied"`
	// Failure names the most recent attempt that did not land.
	Failure string `json:"failure,omitempty"`
	// FailedSequence is the sequence of that attempt.
	FailedSequence uint64 `json:"failedSequence,omitempty"`
}

// Placement is one surface at one commit: both halves and their difference,
// read in the same instant.
//
// Same instant is the point. Read from two places at two moments, a live resize
// turns a correct layer into a drift report and back again depending on which
// read won.
type Placement struct {
	ID         string      `json:"id"`
	Kind       SurfaceKind `json:"kind"`
	Generation uint64      `json:"generation"`
	Layer      int         `json:"layer"`

	Declared        Frame   `json:"declared"`
	DeclaredVisible bool    `json:"declaredVisible"`
	DeclaredAlpha   float64 `json:"declaredAlpha"`

	Applied        Frame   `json:"applied"`
	AppliedVisible bool    `json:"appliedVisible"`
	AppliedAlpha   float64 `json:"appliedAlpha"`

	// Drift is the applied rectangle minus the declared one, per component.
	//
	// Exact, with no tolerance. Both halves are the same float64 travelling one
	// commit, so zero is reachable; a tolerance chosen without a measurement
	// hides the first hundredth of a point of the next coordinate error. A
	// caller that wants to forgive a rounding difference has the number.
	//
	// Nil on an undeclared surface, rather than zero. Zero is the answer for a
	// surface applied exactly where it was declared, and a surface no
	// declaration asked for is the opposite of that. There is no second
	// rectangle to subtract.
	Drift *Frame `json:"drift"`

	// Misparented reports that the surface is in a different window from the
	// one whose document declared it, read off the native object rather than
	// restated from the declaration. Not a difference — a window is not a
	// distance — so it stays out of Drift.
	Misparented bool `json:"misparented"`

	// Undeclared marks a surface the native layer holds that no declaration
	// asked for. It is the defect a ledger-only check cannot find: an
	// application walks its own records, so a surface that left the records and
	// stayed on screen is invisible to every check the application makes.
	Undeclared bool `json:"undeclared"`
}

// Latest answers one window's last commit as a composition.
//
// Per window. One window's inventory is no answer about another's: a
// window-blind reading answers two windows with the same single surface at the
// same rectangle and zero drift while only one of them holds it.
func (service *Service) Latest(window string) Composition {
	service.mu.Lock()
	defer service.mu.Unlock()
	return compositionOf(service.committed[window])
}

// compositionOf pairs the two halves of one commit by surface id.
func compositionOf(commit windowCommit) Composition {
	applied := make(map[string]AppliedSurface, len(commit.applied.Surfaces))
	for _, surface := range commit.applied.Surfaces {
		applied[surface.ID] = surface
	}

	composition := Composition{
		Sequence:       commit.applied.Sequence,
		Failure:        commit.failure,
		FailedSequence: commit.failedSequence,
	}
	declared := make(map[string]bool, len(commit.declared.Surfaces))
	for _, surface := range commit.declared.Surfaces {
		declared[surface.ID] = true
		reported, landed := applied[surface.ID]
		if !landed {
			composition.Unapplied = append(composition.Unapplied, surface.ID)
			continue
		}
		drift := difference(surface.Frame, reported.Frame)
		composition.Surfaces = append(composition.Surfaces, Placement{
			ID:              surface.ID,
			Kind:            surface.Kind,
			Generation:      surface.Generation,
			Layer:           surface.Layer,
			Declared:        surface.Frame,
			DeclaredVisible: surface.Visible,
			DeclaredAlpha:   surface.Alpha,
			Applied:         reported.Frame,
			AppliedVisible:  reported.Visible,
			AppliedAlpha:    reported.Alpha,
			Drift:           &drift,
			Misparented:     reported.Misparented,
		})
	}
	for _, surface := range commit.applied.Surfaces {
		if declared[surface.ID] {
			continue
		}
		composition.Surfaces = append(composition.Surfaces, Placement{
			ID:             surface.ID,
			Generation:     surface.Generation,
			Layer:          surface.Layer,
			Applied:        surface.Frame,
			AppliedVisible: surface.Visible,
			AppliedAlpha:   surface.Alpha,
			Misparented:    surface.Misparented,
			Undeclared:     true,
		})
	}
	return composition
}

// difference is the applied rectangle minus the declared one, per component.
//
// Every component, because a surface that is the right size in the wrong place
// and one that is in the right place at the wrong size are both wrong, and a
// subtraction of the origin alone answers zero for the second.
func difference(declared, applied Frame) Frame {
	return Frame{
		X:      applied.X - declared.X,
		Y:      applied.Y - declared.Y,
		Width:  applied.Width - declared.Width,
		Height: applied.Height - declared.Height,
	}
}
