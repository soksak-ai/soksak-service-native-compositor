import { describe, expect, it } from "vitest";

import { nativeSurfaceDOMRuntime, startNativeSurfaceObserver, type NativeSurfaceObserverRuntime } from "./observer";
import type { NativeSurfaceDeclaration, NativeSurfaceSnapshot } from "./snapshot";

function declaration(id: string, left: () => number): NativeSurfaceDeclaration {
  return {
    dataset: {
      nativeSurface: "browser",
      nativeSurfaceId: id,
      nativeGeneration: "1",
      nativeSource: JSON.stringify({ url: "https://example.com" }),
      nativeLayer: "1",
    },
    isConnected: true,
    getBoundingClientRect: () => ({ left: left(), top: 0, width: 400, height: 600 }),
  };
}

// A document stands in for the window: this package has no DOM in its tests, and what the move
// watch needs from one is a frame clock.
function framed(): ParentNode {
  const view = {
    requestAnimationFrame: (callback: () => void) => setTimeout(callback, 16) as unknown as number,
    cancelAnimationFrame: (handle: number) => clearTimeout(handle),
  };
  return { querySelectorAll: () => [], defaultView: view } as unknown as ParentNode;
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
      observeMoves: () => () => undefined,
      schedule: queueMicrotask,
    }, async (snapshot) => ({ sequence: snapshot.sequence, accepted: true, surfaces: [] }), "win-a");

    await Promise.resolve();
    mutation({ inventoryChanged: false });
    await Promise.resolve();
    expect(resizeStarts).toBe(1);
    expect(resizeStops).toBe(0);
    controller.stop();
  });

  // A pane sliding into the space a sidebar gave up changes no attribute and no size. Measured
  // 2026-08-17 in a three-pane window: the pane travelled 165 → 584 over 190ms while the page stayed
  // at 165 and then appeared at the far end in one frame — 420 points, for 166ms, from the pane it
  // belongs to.
  it("re-declares an element that moved without changing size", async () => {
    let left = 0;
    const element = declaration("browser", () => left);
    const committed: number[] = [];
    const controller = startNativeSurfaceObserver({
      declarations: () => [element],
      observeMutations: () => () => undefined,
      observeResizes: () => () => undefined,
      observeMoves: nativeSurfaceDOMRuntime(framed()).observeMoves,
      schedule: queueMicrotask,
    }, async (snapshot) => {
      committed.push(snapshot.surfaces[0].frame.x);
      return { sequence: snapshot.sequence, accepted: true, surfaces: [] };
    }, "win-a");

    await Promise.resolve();
    await Promise.resolve();
    const first = committed.length;
    left = 584;
    // The move has no event of its own, so the watch is a frame apart from the change.
    await new Promise((done) => setTimeout(done, 60));
    expect(committed.length).toBeGreaterThan(first);
    expect(committed[committed.length - 1]).toBe(584);
    controller.stop();
  });

  it("commits nothing while every declared element is still", async () => {
    const element = declaration("browser", () => 0);
    let commits = 0;
    const controller = startNativeSurfaceObserver({
      declarations: () => [element],
      observeMutations: () => () => undefined,
      observeResizes: () => () => undefined,
      observeMoves: nativeSurfaceDOMRuntime(framed()).observeMoves,
      schedule: queueMicrotask,
    }, async (snapshot) => {
      commits++;
      return { sequence: snapshot.sequence, accepted: true, surfaces: [] };
    }, "win-a");

    await Promise.resolve();
    await Promise.resolve();
    const settled = commits;
    await new Promise((done) => setTimeout(done, 80));
    // A watch that commits every frame would put one round trip per frame on a window nobody is
    // touching.
    expect(commits).toBe(settled);
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
    let mutation!: (change: { inventoryChanged: boolean }) => void;
    let resize!: () => void;
    let release = deferred();
    const secondStarted = deferred();
    const commits: NativeSurfaceSnapshot[] = [];
    const runtime: NativeSurfaceObserverRuntime = {
      declarations: () => [element],
      observeMutations: (callback) => { mutation = callback; return () => undefined; },
      observeResizes: (_elements, callback) => { resize = callback; return () => undefined; },
      observeMoves: () => () => undefined,
      schedule: queueMicrotask,
    };
    const controller = startNativeSurfaceObserver(runtime, async (snapshot) => {
      commits.push(snapshot);
      if (commits.length === 2) secondStarted.resolve();
      await release.promise;
      return { sequence: snapshot.sequence, accepted: true, surfaces: [] };
    }, "win-a");

    await Promise.resolve();
    expect(commits).toHaveLength(1);
    x = 100;
    resize();
    x = 200;
    mutation({ inventoryChanged: true });
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
    let mutation!: (change: { inventoryChanged: boolean }) => void;
    let resize!: () => void;
    let mutationStops = 0;
    let resizeStops = 0;
    const commits: NativeSurfaceSnapshot[] = [];
    const controller = startNativeSurfaceObserver({
      declarations: () => [element],
      observeMutations: (callback) => { mutation = callback; return () => { mutationStops++; }; },
      observeResizes: (_elements, callback) => { resize = callback; return () => { resizeStops++; }; },
      observeMoves: () => () => undefined,
      schedule: queueMicrotask,
    }, async (snapshot) => {
      commits.push(snapshot);
      return { sequence: snapshot.sequence, accepted: true, surfaces: [] };
    }, "win-a");

    await Promise.resolve();
    controller.stop();
    mutation({ inventoryChanged: true });
    resize();
    await Promise.resolve();
    expect(mutationStops).toBe(1);
    expect(resizeStops).toBe(1);
    expect(commits).toHaveLength(1);
  });
});
