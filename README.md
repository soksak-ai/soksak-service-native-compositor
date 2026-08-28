# Wails Native Compositor

`wails-service-native-compositor` makes DOM-declared native child surfaces follow one
validated inventory contract. Applications declare surfaces; this package owns
observation, sequencing, stale rejection, and applied receipts. A native
surface plugin implements the public Go `Backend` interface and owns its native
technology. `soksak-plugin-browser-native`, for example, owns AppKit and WKWebView.

## Contract

The application declares a browser host with public attributes:

```html
<div
  data-native-surface="browser"
  data-native-surface-id="browser-1"
  data-native-generation="1"
  data-native-source='{"url":"https://example.com"}'
  data-native-visible="true"
  data-native-alpha="1"
  data-native-layer="10"
></div>
```

The frontend package observes declarations with `MutationObserver` and
`ResizeObserver`, forms one full inventory snapshot, and calls an injected
`commit(snapshot)` interface. It does not import Wails-generated bindings.

The Go service exposes:

- `Commit(Snapshot) (Receipt, error)` — the only native writer;
- `Status(window) Receipt` — one window's latest accepted applied inventory;
- `Latest(window) Composition` — one window's last commit read as a comparison:
  the declared rectangle, the applied one, and the difference per surface, plus
  the surfaces only one half holds and the reason the last attempt did not land.
  Both halves come from that one commit, which is what makes the difference a
  subtraction rather than two readings taken at two moments.

A snapshot with a stale sequence is rejected without reaching the injected
backend. The backend returns the complete applied inventory in one receipt.

### Transaction and lock ownership

`Commit`, `Deliver`, and `Drain` are backend-writer transactions and are
serialised with each other. The lock protecting receipts, compositions,
history, and hit-testing state is never held while resolving a native window or
calling a backend. Those are foreign boundaries and may synchronously enter a
platform UI thread; that thread must remain free to call `Status`, `Latest`, or
`SurfaceAt` without forming a lock cycle.

While a backend transaction is in progress, readers see the last completed
snapshot. A successful backend result becomes the next completed snapshot in
one state update. Backend implementations must not recursively invoke a writer
transaction; they return their result through `Backend` and let the compositor
publish it.

The host application registers the service with Wails and injects the generated
`Commit` binding into `startNativeSurfaceObserver`. No application-specific
layout tree, component, or generated binding path exists in this package.

## Verification

```sh
make verify
```

`go.mod`, `.node-version`, and `package.json#packageManager` are the exact Go, Node, and pnpm owners.
Preflight resolves pnpm from this repository root and judges the effective version reported there. It
does not inspect a globally installed pnpm package behind the selected launcher.
Make verifies only the public compositor service and DOM observer implementation; applications and
native Backend implementations verify their own boundaries.

The complete build rule is in [`docs/BUILD.md`](docs/BUILD.md). Completed changes are recorded in
[`docs/CHANGELOG.md`](docs/CHANGELOG.md); the changelog does not define current behavior.
