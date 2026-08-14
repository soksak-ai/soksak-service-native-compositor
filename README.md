# Wails Native Compositor

`wails-native-compositor` makes DOM-declared native child surfaces follow one
validated inventory contract. Applications declare surfaces; this package owns
observation, sequencing, stale rejection, and applied receipts. A native
surface plugin implements the public Go `Backend` interface and owns its native
technology. `soksak-browser-native`, for example, owns AppKit and WKWebView.

## Contract

The application declares a browser host with public attributes:

```html
<div
  data-wails-native-surface="browser"
  data-native-surface-id="browser-1"
  data-native-generation="1"
  data-native-url="https://example.com"
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
- `Status() Receipt` — the latest accepted applied inventory.

A snapshot with a stale sequence is rejected without reaching the injected
backend. The backend returns the complete applied inventory in one receipt.

The host application registers the service with Wails and injects the generated
`Commit` binding into `startNativeSurfaceObserver`. No application-specific
layout tree, component, or generated binding path exists in this package.

## Verification

```sh
go test ./...
go vet ./...
pnpm --dir frontend test
pnpm --dir frontend typecheck
```
