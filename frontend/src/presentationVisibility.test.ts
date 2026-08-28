// @vitest-environment jsdom
import { describe, expect, it, vi } from "vitest";

import { nativeSurfaceDOMRuntime, startNativeSurfaceObserver } from "./observer";
import type { NativeSurfaceSnapshot } from "./snapshot";

const settle = () => new Promise<void>((resolve) => setTimeout(resolve, 0));

vi.stubGlobal("ResizeObserver", class {
  observe() {}
  disconnect() {}
});

describe("host presentation visibility", () => {
  it("hides a declared surface while its host layout ancestor is surface-hidden", async () => {
    const host = document.createElement("div");
    host.dataset.surfaceVisible = "false";
    const surface = document.createElement("div");
    surface.dataset.nativeSurface = "terminal";
    surface.dataset.nativeSurfaceId = "terminal.win-a.tab-a-1";
    surface.dataset.nativeGeneration = "1";
    surface.dataset.nativeSource = JSON.stringify({ pane: "tab-a.1" });
    surface.dataset.nativeVisible = "true";
    surface.dataset.nativeAlpha = "1";
    surface.dataset.nativeLayer = "0";
    surface.getBoundingClientRect = () => ({
      x: 0, y: 0, left: 0, top: 0, right: 400, bottom: 300,
      width: 400, height: 300, toJSON: () => ({}),
    });
    host.append(surface);
    document.body.append(host);

    const committed: NativeSurfaceSnapshot[] = [];
    const controller = startNativeSurfaceObserver(
      nativeSurfaceDOMRuntime(document),
      async (snapshot) => {
        committed.push(snapshot);
        return { sequence: snapshot.sequence, accepted: true, surfaces: snapshot.surfaces };
      },
      "win-a",
    );
    await settle();
    expect(committed.at(-1)?.surfaces[0].visible).toBe(false);

    host.dataset.surfaceVisible = "true";
    await settle();
    expect(committed.at(-1)?.surfaces[0].visible).toBe(true);
    controller.stop();
  });
});
