package nativesurface

// wiredFor puts one backend behind the kinds a test declares, and behind no others.
//
// The compositor takes a backend per kind, which is what makes a second kind a sibling rather than
// an edit to it. A test that wired one backend for every kind would receive one call per kind — the
// complete inventory each of them is owed — and would then be asserting about a shape no real
// wiring has.
//
// The names a test passes are deliberately assorted across this package. A compositor that
// recognised "browser" would be one with an opinion about what a browser is, and every one of these
// reads to it as the string it is.
func wiredFor(backend Backend, kinds ...SurfaceKind) map[SurfaceKind]Backend {
	wired := make(map[SurfaceKind]Backend, len(kinds))
	for _, kind := range kinds {
		wired[kind] = backend
	}
	return wired
}
