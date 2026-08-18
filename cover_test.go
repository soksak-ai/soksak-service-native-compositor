package nativesurface

import "testing"

// What of a surface a person can see.
//
// A window's presence says nothing about one surface inside it. Two surfaces in one window overlap,
// and the one behind holds its rectangle, its generation and zero drift while nobody can see any of
// it — every reading in this build answered correct for a surface that was completely hidden,
// because none of them asked.

// shown is one applied, visible, opaque surface — `placed` beside surfaceat_test, which is where
// the shape belongs, with the two flags these cases never vary.
func shown(id string, layer int, x, y, w, h float64) Placement {
	return placed(id, layer, x, y, w, h, true, 1)
}

func TestASurfaceNothingIsOverIsNotCovered(t *testing.T) {
	beside := composition(shown("a", 0, 0, 0, 100, 100), shown("b", 0, 200, 0, 100, 100))
	for _, id := range []string{"a", "b"} {
		cover := coverOf(beside, beside.Surfaces[map[string]int{"a": 0, "b": 1}[id]])
		if cover.Fraction != 0 || len(cover.By) != 0 {
			t.Errorf("%s stands beside the other and is covered %.2f by %v", id, cover.Fraction, cover.By)
		}
	}
}

func TestASurfaceUnderAnotherIsCoveredWhole(t *testing.T) {
	// The case the readings could not tell from a correct one: same rectangle, one in front. The
	// one behind keeps a right-looking frame, generation and drift.
	stacked := composition(shown("under", 0, 0, 0, 100, 100), shown("over", 1, 0, 0, 100, 100))

	under := coverOf(stacked, stacked.Surfaces[0])
	if under.Fraction != 1 {
		t.Errorf("a surface entirely behind another is covered %.2f, not all of it", under.Fraction)
	}
	if len(under.By) != 1 || under.By[0] != "over" {
		t.Errorf("what covers it is %v, and the answer has to name it", under.By)
	}

	over := coverOf(stacked, stacked.Surfaces[1])
	if over.Fraction != 0 || len(over.By) != 0 {
		t.Errorf("the surface in front is covered %.2f by %v", over.Fraction, over.By)
	}
}

func TestHalfCoveredIsHalf(t *testing.T) {
	// A pane covered down one side is the reading a person reports as "the page is cut off", and a
	// fraction is what tells it from a pane that is simply behind everything.
	half := composition(shown("under", 0, 0, 0, 100, 100), shown("over", 1, 50, 0, 50, 100))
	cover := coverOf(half, half.Surfaces[0])
	if cover.Fraction < 0.45 || cover.Fraction > 0.55 {
		t.Errorf("half of it is behind the other and the reading is %.2f", cover.Fraction)
	}
}

func TestASurfaceNobodyAskedToDrawIsNotCovered(t *testing.T) {
	// Not applied-visible, or transparent, is a different fact and it is already reported. Folding
	// the two together would answer "hidden" for a surface that was never asked for.
	hidden := placed("hidden", 0, 0, 0, 100, 100, false, 1)
	clear := placed("clear", 0, 0, 0, 100, 100, true, 0)
	stack := composition(hidden, clear, shown("over", 1, 0, 0, 100, 100))

	for index, what := range []string{"an invisible surface", "a transparent surface"} {
		if cover := coverOf(stack, stack.Surfaces[index]); cover.Fraction != 0 {
			t.Errorf("%s reads as covered %.2f; it is not there rather than hidden", what, cover.Fraction)
		}
	}
}

func TestTheOrderIsSurfaceAtsOwn(t *testing.T) {
	// Sampled through surfaceAt rather than compared here, so what a point belongs to and what
	// covers a surface cannot answer differently. Equal layers: the later declaration is in front,
	// which is the order the inventory composites in.
	equal := composition(shown("first", 0, 0, 0, 100, 100), shown("second", 0, 0, 0, 100, 100))
	if cover := coverOf(equal, equal.Surfaces[0]); cover.Fraction != 1 {
		t.Errorf("the earlier declaration reads as covered %.2f by an equal layer over it", cover.Fraction)
	}
	if cover := coverOf(equal, equal.Surfaces[1]); cover.Fraction != 0 {
		t.Errorf("the later declaration reads as covered %.2f", cover.Fraction)
	}
}
