import { describe, expect, it } from "vitest";

import { nativeSurfaceDOMRuntime, startNativeSurfaceObserver, type NativeSurfaceObserverRuntime } from "./observer";
import type { NativeSurfaceDeclaration, NativeSurfaceSnapshot } from "./snapshot";

function declaration(id: string, left: () => number): NativeSurfaceDeclaration {
  return {
    dataset: {
      wailsNativeSurface: "browser",
      nativeSurfaceId: id,
      nativeGeneration: "1",
      nativeSource: JSON.stringify({ url: "https://example.com" }),
      nativeLayer: "1",
    },
    isConnected: true,
    getBoundingClientRect: () => ({ left: left(), top: 0, width: 400, height: 600 }),
  };
}

function deferred() {
  let resolve!: () => void;
  const promise = new Promise<void>((done) => { resolve = done; });
  return { promise, resolve };
}

describe("native surface observer", () => {
  it("does not replace resize ownership for geometry-only mutations", async () => {
    const element = declaration("browser", () => 0);
    let mutation!: (change: { inventoryChanged: boolean }) => void;
    let resizeStarts = 0;
    let resizeStops = 0;
    const controller = startNativeSurfaceObserver({
      declarations: () => [element],
      observeMutations: (callback) => { mutation = callback; return () => undefined; },
      observeResizes: () => { resizeStarts++; return () => { resizeStops++; }; },
      schedule: queueMicrotask,
    }, async (snapshot) => ({ sequence: snapshot.sequence, accepted: true, surfaces: [] }));

    await Promise.resolve();
    mutation({ inventoryChanged: false });
    await Promise.resolve();
    expect(resizeStarts).toBe(1);
    expect(resizeStops).toBe(0);
    controller.stop();
  });

  it("calls the host microtask scheduler without changing its receiver", () => {
    const receivers: unknown[] = [];
    let called = 0;
    const hostSchedule = function (this: unknown, callback: () => void) {
      receivers.push(this);
      callback();
    };
    const runtime = nativeSurfaceDOMRuntime({} as ParentNode, hostSchedule);
    runtime.schedule(() => { called++; });
    expect(receivers).toEqual([undefined]);
    expect(called).toBe(1);
  });

  it("serializes whole inventories and coalesces bursts to the latest layout", async () => {
    let x = 0;
    const element = declaration("browser", () => x);
    let mutation!: () => void;
    let resize!: () => void;
    let release = deferred();
    const secondStarted = deferred();
    const commits: NativeSurfaceSnapshot[] = [];
    const runtime: NativeSurfaceObserverRuntime = {
      declarations: () => [element],
      observeMutations: (callback) => { mutation = callback; return () => undefined; },
      observeResizes: (_elements, callback) => { resize = callback; return () => undefined; },
      schedule: queueMicrotask,
    };
    const controller = startNativeSurfaceObserver(runtime, async (snapshot) => {
      commits.push(snapshot);
      if (commits.length === 2) secondStarted.resolve();
      await release.promise;
      return { sequence: snapshot.sequence, accepted: true, surfaces: [] };
    });

    await Promise.resolve();
    expect(commits).toHaveLength(1);
    x = 100;
    resize();
    x = 200;
    mutation();
    await Promise.resolve();
    expect(commits).toHaveLength(1);

    release.resolve();
    release = deferred();
    await secondStarted.promise;
    expect(commits).toHaveLength(2);
    expect(commits[1].surfaces[0].frame.x).toBe(200);
    expect(commits[1].sequence).toBeGreaterThan(commits[0].sequence);
    controller.stop();
  });

  it("disconnects both event owners and publishes no later snapshot after stop", async () => {
    const element = declaration("browser", () => 0);
    let mutation!: () => void;
    let resize!: () => void;
    let mutationStops = 0;
    let resizeStops = 0;
    const commits: NativeSurfaceSnapshot[] = [];
    const controller = startNativeSurfaceObserver({
      declarations: () => [element],
      observeMutations: (callback) => { mutation = callback; return () => { mutationStops++; }; },
      observeResizes: (_elements, callback) => { resize = callback; return () => { resizeStops++; }; },
      schedule: queueMicrotask,
    }, async (snapshot) => {
      commits.push(snapshot);
      return { sequence: snapshot.sequence, accepted: true, surfaces: [] };
    });

    await Promise.resolve();
    controller.stop();
    mutation();
    resize();
    await Promise.resolve();
    expect(mutationStops).toBe(1);
    expect(resizeStops).toBe(1);
    expect(commits).toHaveLength(1);
  });
});
