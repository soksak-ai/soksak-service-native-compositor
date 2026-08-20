package nativesurface

import (
	"errors"
	"strings"
	"testing"
	"unsafe"
)

// A surface has to be driveable, and only the backend that owns it knows how.
//
// The declaration places a surface and the compositor keeps the inventory, but a browser also has
// to be told to go back, and a video surface to seek. Those verbs belong to the kind, not here —
// a compositor that knew them would need editing for every new kind, which is the A4 lock-in the
// substrate exists to prevent. So the message travels closed: this validates who it is for and
// forwards it without reading it.
type deliveringBackend struct {
	recordingBackend
	delivered []deliveredMessage
	answer    map[string]any
	err       error
}

type deliveredMessage struct {
	id      string
	message map[string]any
}

func (backend *deliveringBackend) Deliver(id string, message map[string]any) (map[string]any, error) {
	backend.delivered = append(backend.delivered, deliveredMessage{id: id, message: message})
	return backend.answer, backend.err
}

func deliveringService(t *testing.T, backend *deliveringBackend, ids ...string) *Service {
	t.Helper()
	// A real allocation, not a made-up address — checkptr rejects arithmetic on an invented one.
	window := unsafe.Pointer(new(byte))
	service := NewService(func(string) unsafe.Pointer { return window }, wiredFor(backend, "browser"))
	surfaces := make([]Surface, 0, len(ids))
	for _, id := range ids {
		surfaces = append(surfaces, Surface{
			ID: id, Kind: "browser", Generation: 1, Alpha: 1, Visible: true,
			Frame:  Frame{X: 0, Y: 0, Width: 100, Height: 100},
			Source: map[string]string{"url": "https://example.com"},
		})
	}
	if _, err := service.Commit(Snapshot{Window: "win-a", Sequence: 1, Surfaces: surfaces}); err != nil {
		t.Fatalf("committing the fixture inventory: %v", err)
	}
	return service
}

func TestAMessageReachesTheBackendUnread(t *testing.T) {
	backend := &deliveringBackend{answer: map[string]any{"url": "https://example.org"}}
	service := deliveringService(t, backend, "brw-a")

	answer, err := service.Deliver("brw-a", map[string]any{"verb": "navigate", "url": "https://example.org"})
	if err != nil {
		t.Fatalf("delivering: %v", err)
	}
	if len(backend.delivered) != 1 {
		t.Fatalf("the backend received %d messages, not 1", len(backend.delivered))
	}
	got := backend.delivered[0]
	if got.id != "brw-a" {
		t.Errorf("the message went to %q", got.id)
	}
	if got.message["verb"] != "navigate" || got.message["url"] != "https://example.org" {
		t.Errorf("the message arrived as %v", got.message)
	}
	if answer["url"] != "https://example.org" {
		t.Errorf("the backend's answer came back as %v", answer)
	}
}

func TestAMessageForASurfaceThatIsNotInTheInventoryIsRefused(t *testing.T) {
	// The inventory is the only record of what exists. Forwarding a message for an id nobody
	// declared asks the backend to invent a surface, and a backend that obliges holds one the
	// compositor does not know about — the undeclared surface a ledger-only check cannot see.
	backend := &deliveringBackend{}
	service := deliveringService(t, backend, "brw-a")

	if _, err := service.Deliver("brw-ghost", map[string]any{"verb": "reload"}); err == nil {
		t.Fatal("a message for an undeclared surface was forwarded")
	}
	if len(backend.delivered) != 0 {
		t.Errorf("the backend was asked anyway: %v", backend.delivered)
	}
}

func TestAMessageAfterShutdownIsRefused(t *testing.T) {
	// Shutdown applies one empty inventory, so there is nothing left to drive. A message accepted
	// afterwards would rebuild what shutdown destroyed.
	backend := &deliveringBackend{}
	service := deliveringService(t, backend, "brw-a")
	if err := service.ServiceShutdown(); err != nil {
		t.Fatalf("shutting down: %v", err)
	}
	_, err := service.Deliver("brw-a", map[string]any{"verb": "reload"})
	if err == nil {
		t.Fatal("a message was delivered after shutdown")
	}
	// Named, because "not in the inventory" is also true here and sends the reader looking for a
	// surface that was destroyed on purpose.
	if !strings.Contains(err.Error(), "shut down") {
		t.Errorf("the refusal reads %q; it does not say the compositor is gone", err)
	}
}

func TestTheBackendsRefusalComesBackByName(t *testing.T) {
	// A verb the kind does not answer is the backend's to refuse. Swallowing it here would leave a
	// caller believing a browser went back when it did not.
	backend := &deliveringBackend{err: errRefused}
	service := deliveringService(t, backend, "brw-a")
	if _, err := service.Deliver("brw-a", map[string]any{"verb": "seek"}); err == nil {
		t.Fatal("the backend refused and the caller was told it worked")
	}
}

// errRefused stands for whatever the kind's backend says when it will not do something.
var errRefused = errors.New("this surface kind answers no such verb")
