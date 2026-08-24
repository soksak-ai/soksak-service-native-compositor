package nativesurface

import "sort"

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
	// AppliedAtUnixMs is when the backend finished applying this exact inventory.
	AppliedAtUnixMs float64 `json:"appliedAtUnixMs"`
	// Interactive is the explicit layout phase carried by this commit.
	Interactive bool `json:"interactive"`
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

	Applied                   Frame   `json:"applied"`
	Settled                   *Frame  `json:"settled,omitempty"`
	LayerContentsRedrawPolicy int     `json:"layerContentsRedrawPolicy"`
	LayerContentsPlacement    int     `json:"layerContentsPlacement"`
	AppliedVisible            bool    `json:"appliedVisible"`
	AppliedAlpha              float64 `json:"appliedAlpha"`

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

// History answers the last retained successful application before sinceUnixMs as a baseline, then
// every application at or after it, in apply order. A DOM trace whose first frame precedes the first
// motion Apply still needs to know what the native layer already held; omitting that baseline calls
// the first displayed frame unobserved. An exact timestamp starts at the first sample carrying that
// timestamp. Wall-clock milliseconds are not a unique cursor: multiple applies can share one, and
// dropping an equal-time sample would make the trace lossy. Sequence preserves their apply order.
//
// The compositor owns this timeline because it is the only layer that observes Apply itself;
// reconstructing it from frontend responses measures bridge return order instead.
func (service *Service) History(window string, sinceUnixMs float64) []Composition {
	service.mu.Lock()
	defer service.mu.Unlock()
	commits := service.history[window]
	start := 0
	for start < len(commits) && commits[start].applied.AppliedAtUnixMs < sinceUnixMs {
		start++
	}
	if start > 0 && (start == len(commits) || commits[start].applied.AppliedAtUnixMs != sinceUnixMs) {
		start--
	}
	out := make([]Composition, 0, len(commits)-start)
	for _, commit := range commits[start:] {
		out = append(out, compositionOf(commit))
	}
	return out
}

// compositionOf pairs the two halves of one commit by surface id.
func compositionOf(commit windowCommit) Composition {
	applied := make(map[string]AppliedSurface, len(commit.applied.Surfaces))
	for _, surface := range commit.applied.Surfaces {
		applied[surface.ID] = surface
	}

	composition := Composition{
		Sequence:        commit.applied.Sequence,
		AppliedAtUnixMs: commit.applied.AppliedAtUnixMs,
		Interactive:     commit.declared.Interactive,
		Failure:         commit.failure,
		FailedSequence:  commit.failedSequence,
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
			ID:                        surface.ID,
			Kind:                      surface.Kind,
			Generation:                surface.Generation,
			Layer:                     surface.Layer,
			Declared:                  surface.Frame,
			DeclaredVisible:           surface.Visible,
			DeclaredAlpha:             surface.Alpha,
			Applied:                   reported.Frame,
			Settled:                   reported.Settled,
			LayerContentsRedrawPolicy: reported.LayerContentsRedrawPolicy,
			LayerContentsPlacement:    reported.LayerContentsPlacement,
			AppliedVisible:            reported.Visible,
			AppliedAlpha:              reported.Alpha,
			Drift:                     &drift,
			Misparented:               reported.Misparented,
		})
	}
	for _, surface := range commit.applied.Surfaces {
		if declared[surface.ID] {
			continue
		}
		composition.Surfaces = append(composition.Surfaces, Placement{
			ID:                        surface.ID,
			Generation:                surface.Generation,
			Layer:                     surface.Layer,
			Applied:                   surface.Frame,
			Settled:                   surface.Settled,
			LayerContentsRedrawPolicy: surface.LayerContentsRedrawPolicy,
			LayerContentsPlacement:    surface.LayerContentsPlacement,
			AppliedVisible:            surface.Visible,
			AppliedAlpha:              surface.Alpha,
			Misparented:               surface.Misparented,
			Undeclared:                true,
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

// SurfaceAt names the surface a point lands on, or "" for a point on none.
//
// A surface is composited above the document, so a click inside one is delivered to it and the page
// above never sees it. Whoever needs to know which surface that was has the point and nothing else,
// and this service is what holds every surface's rectangle in the coordinate contract they are
// declared in (A2, CSS top-left). Walking the native view tree instead re-derives that fact in
// whatever coordinate space the walker happens to be in — measured 2026-08-17, a first attempt did
// exactly that and landed short by the title bar's height.
//
// The applied rectangle, not the declared one: the point came from the screen, and what is on the
// screen is what the native layer applied.
//
// Topmost wins, by layer and then by the order the inventory declared them, which is the order they
// are composited in. A point inside two overlapping surfaces belongs to the one a person sees.
func (service *Service) SurfaceAt(window string, x float64, y float64) string {
	service.mu.Lock()
	defer service.mu.Unlock()
	return surfaceAt(compositionOf(service.committed[window]), x, y)
}

func surfaceAt(composition Composition, x float64, y float64) string {
	found := ""
	layer := 0
	for _, placement := range composition.Surfaces {
		// Invisible or transparent is not there to be clicked. A surface parked behind the one a
		// person is looking at still holds its rectangle, which is what keeps its layout.
		if !placement.AppliedVisible || placement.AppliedAlpha <= 0 {
			continue
		}
		frame := placement.Applied
		if x < frame.X || y < frame.Y || x >= frame.X+frame.Width || y >= frame.Y+frame.Height {
			continue
		}
		if found == "" || placement.Layer >= layer {
			found = placement.ID
			layer = placement.Layer
		}
	}
	return found
}

// What of a surface a person can see, and what is over it.
//
// A window's own presence — visible, key, not covered by another application — says nothing about
// one surface inside it. Two surfaces in one window overlap, and the one behind holds its rectangle,
// its generation and zero drift while nobody can see any of it. Measured across this build: every
// reading answered correct for a surface that was completely hidden, because none of them asked.
//
// The rule is SurfaceAt's, sampled rather than restated: topmost by layer, then by the order the
// inventory declared them. Asking the same function about points inside a surface is what makes
// this answer and that answer the same answer.

// coverSamples is how many points across a surface are asked about, per axis. Nine hundred points
// for a surface, which resolves a hidden strip about three per cent of its width — finer than that
// is a fraction nobody acts on, and coarser misses a pane covered down one side.
const coverSamples = 30

// Cover is what lies over one surface.
type Cover struct {
	// By names the surfaces found over this one, in no order. Empty means every point sampled
	// inside it belongs to it.
	By []string `json:"by"`
	// Fraction is how much of it they hold, 0 through 1, sampled. Exactly 1 is every sampled point
	// belonging to something else, which is a surface nobody can see any of.
	Fraction float64 `json:"fraction"`
}

// coverOf samples one surface against the composition it is in.
//
// A surface that is not applied-visible or is transparent is not covered — it is not there. That is
// a different fact and it is already reported; folding the two together would answer "hidden"
// for a surface nobody asked to draw.
func coverOf(composition Composition, subject Placement) Cover {
	cover := Cover{By: []string{}}
	if !subject.AppliedVisible || subject.AppliedAlpha <= 0 {
		return cover
	}
	if subject.Applied.Width <= 0 || subject.Applied.Height <= 0 {
		return cover
	}
	over := map[string]bool{}
	covered := 0
	total := 0
	for row := 0; row < coverSamples; row++ {
		for column := 0; column < coverSamples; column++ {
			// The centre of each cell rather than its corner: a corner on the boundary between two
			// surfaces belongs to whichever the comparison rounds toward, and the answer would
			// change with the arithmetic rather than with the screen.
			x := subject.Applied.X + subject.Applied.Width*(float64(column)+0.5)/coverSamples
			y := subject.Applied.Y + subject.Applied.Height*(float64(row)+0.5)/coverSamples
			total++
			at := surfaceAt(composition, x, y)
			if at == "" || at == subject.ID {
				continue
			}
			covered++
			over[at] = true
		}
	}
	for id := range over {
		cover.By = append(cover.By, id)
	}
	sort.Strings(cover.By)
	if total > 0 {
		cover.Fraction = float64(covered) / float64(total)
	}
	return cover
}

// CoverIn answers what lies over every surface of one window, keyed by surface id.
func (service *Service) CoverIn(window string) map[string]Cover {
	service.mu.Lock()
	defer service.mu.Unlock()
	composition := compositionOf(service.committed[window])
	covers := make(map[string]Cover, len(composition.Surfaces))
	for _, placement := range composition.Surfaces {
		covers[placement.ID] = coverOf(composition, placement)
	}
	return covers
}
