import { describe, expect, it } from "vitest";

import { collectNativeSurfaceSnapshot } from "./snapshot";

describe("declarative native surface inventory", () => {
  it("collects one ordered snapshot from public element declarations", () => {
    const elements = [
      declaration("right", 1, { left: 400, top: 0, width: 400, height: 600 }),
      declaration("left", 2, { left: 0, top: 0, width: 400, height: 600 }),
    ];

    expect(collectNativeSurfaceSnapshot(elements, 7)).toEqual({
      sequence: 7,
      surfaces: [
        { id: "left", generation: 2, kind: "browser", frame: { x: 0, y: 0, width: 400, height: 600 }, visible: true, alpha: 1, layer: 10, source: { url: "https://example.com/left" } },
        { id: "right", generation: 1, kind: "browser", frame: { x: 400, y: 0, width: 400, height: 600 }, visible: true, alpha: 1, layer: 20, source: { url: "https://example.com/right" } },
      ],
    });
  });

  it("rejects duplicate IDs instead of selecting an arbitrary writer", () => {
    const elements = [
      declaration("same", 1, { left: 0, top: 0, width: 10, height: 10 }),
      declaration("same", 2, { left: 10, top: 0, width: 10, height: 10 }),
    ];
    expect(() => collectNativeSurfaceSnapshot(elements, 1)).toThrow("duplicate native surface id: same");
  });
});

function declaration(id: string, generation: number, rect: { left: number; top: number; width: number; height: number }) {
  return {
    dataset: {
      wailsNativeSurface: "browser",
      nativeSurfaceId: id,
      nativeGeneration: String(generation),
      nativeSource: JSON.stringify({ url: `https://example.com/${id}` }),
      nativeLayer: id === "left" ? "10" : "20",
    },
    isConnected: true,
    getBoundingClientRect: () => rect,
  };
}
