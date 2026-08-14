// @vitest-environment jsdom
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

  it("keeps project-defined native surface kinds opaque", () => {
    const element = declaration("custom", 1, { left: 0, top: 0, width: 10, height: 10 }, "project-native-kind");
    expect(collectNativeSurfaceSnapshot([element], 1).surfaces[0].kind).toBe("project-native-kind");
  });
});

// The fixture is built from real attributes, not from a hand-written dataset.
// A hand-written dataset lets the reader and the declaration drift apart while
// every test stays green — which is exactly what happened here once.
function declaration(id: string, generation: number, rect: { left: number; top: number; width: number; height: number }, kind = "browser") {
  const element = document.createElement("div");
  element.setAttribute("data-native-surface", kind);
  element.setAttribute("data-native-surface-id", id);
  element.setAttribute("data-native-generation", String(generation));
  element.setAttribute("data-native-source", JSON.stringify({ url: `https://example.com/${id}` }));
  element.setAttribute("data-native-layer", id === "left" ? "10" : "20");
  return {
    dataset: element.dataset,
    isConnected: true,
    getBoundingClientRect: () => rect,
  };
}
