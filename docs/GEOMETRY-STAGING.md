# Geometry staging

`stageGeometry` applies a finite set of declared surface rectangles before the document publishes
the matching layout commit. The controller returns the compositor receipt for that full inventory.
The caller publishes the DOM layout only after the receipt succeeds.

The controller retains the staged rectangles while unrelated mutation and resize notifications run.
The caller ends the lease with `releaseGeometry` once it has published the DOM layout; the
controller then commits the measured rectangles and later DOM changes use measured rectangles again.

The lease does not end on a measurement. A staged rectangle is the caller's prediction of a layout
the document has not made yet, and the element that declares the surface is laid out by its own
owner — a terminal plugin sizes its element to whole pixels while the pane it fills is fractional.
Measured 2026-09-05: a rectangle staged 160.26 wide was laid out 160 wide, a lease that waited for
the two to agree held the surface at the staged rectangle for the rest of the session, and every
later layout change moved the document and not the surface.

The command rejects an undeclared surface id or a rectangle containing a negative or non-finite value.
It does not inspect provider internals and does not infer surface relationships.
