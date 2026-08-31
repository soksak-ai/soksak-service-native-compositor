# Geometry staging

`stageGeometry` applies a finite set of declared surface rectangles before the document publishes
the matching layout commit. The controller returns the compositor receipt for that full inventory.
The caller publishes the DOM layout only after the receipt succeeds.

The controller retains the staged rectangles while unrelated mutation and resize notifications run.
It removes the geometry lease when every addressed declaration reports the same rectangle. Later DOM
changes then use measured rectangles again.

The command rejects an undeclared surface id or a rectangle containing a negative or non-finite value.
It does not inspect provider internals and does not infer surface relationships.
