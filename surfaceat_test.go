package nativesurface

import "testing"

// Which surface a point lands on.
//
// A surface is composited above the document, so a click inside one is delivered to it and the page
// above never sees it. Whoever needs to know which surface that was holds the point and nothing
// else, and this service holds every rectangle in the contract they are declared in.
//
// The first attempt at this walked the native view tree from a plugin instead, and landed short by
// the title bar's height because `hitTest:` takes its point in the receiver's superview coordinates
// (measured 2026-08-17). A rectangle test over numbers this service already has needs no window and
// no coordinate space of its own.
func composition(placements ...Placement) Composition {
	return Composition{Surfaces: placements}
}

func placed(id string, layer int, x, y, w, h float64, visible bool, alpha float64) Placement {
	return Placement{
		ID:             id,
		Layer:          layer,
		Applied:        Frame{X: x, Y: y, Width: w, Height: h},
		AppliedVisible: visible,
		AppliedAlpha:   alpha,
	}
}

func TestTheSurfaceAPointLandsOn(t *testing.T) {
	left := placed("left", 0, 0, 0, 400, 600, true, 1)
	right := placed("right", 0, 400, 0, 400, 600, true, 1)
	over := placed("over", 5, 350, 100, 100, 100, true, 1)
	hidden := placed("hidden", 9, 0, 0, 800, 600, false, 1)
	clear := placed("clear", 9, 0, 0, 800, 600, true, 0)

	for _, probe := range []struct {
		name string
		x, y float64
		want string
	}{
		{"inside the left one", 10, 10, "left"},
		{"inside the right one", 500, 300, "right"},
		{"its top-left corner is inside it", 400, 0, "right"},
		{"its bottom-right corner is outside it", 800, 600, ""},
		{"between them by a point is the right one", 399.9, 0, "left"},
		{"above both is the one on top", 380, 150, "over"},
		{"outside every rectangle is no surface", 900, 900, ""},
		{"a surface that is not visible is not there to be clicked", 700, 550, "right"},
	} {
		t.Run(probe.name, func(t *testing.T) {
			// The one on top is declared first here on purpose: taking the last match instead of the
			// highest layer would answer the same as this rule if it came last, and the probe would
			// pass against a build that never reads the layer.
			got := surfaceAt(composition(over, hidden, clear, left, right), probe.x, probe.y)
			if got != probe.want {
				t.Errorf("the point (%g, %g) landed on %q, and the surface a person sees there is %q",
					probe.x, probe.y, got, probe.want)
			}
		})
	}
}

func TestAWindowWithNoCommitLandsOnNothing(t *testing.T) {
	// A window whose document declared no surface, and one the compositor has never run for, both
	// answer the same here: there is nothing under the point.
	if got := surfaceAt(Composition{}, 10, 10); got != "" {
		t.Errorf("a window with no commit answered %q", got)
	}
}
