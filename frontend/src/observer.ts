import { collectNativeSurfaceSnapshot, type NativeSurfaceDeclaration, type NativeSurfaceSnapshot } from "./snapshot";

export type NativeSurfaceReceipt = {
  sequence: number;
  accepted: boolean;
  surfaces: ReadonlyArray<unknown>;
};

export type NativeSurfaceCommit = (snapshot: NativeSurfaceSnapshot) => Promise<NativeSurfaceReceipt>;
export type NativeSurfaceMutation = { inventoryChanged: boolean };
export type NativeSurfaceObserverRuntime = {
  declarations(): Iterable<NativeSurfaceDeclaration>;
  observeMutations(callback: (mutation: NativeSurfaceMutation) => void): () => void;
  observeResizes(declarations: Iterable<NativeSurfaceDeclaration>, callback: () => void): () => void;
  /**
   * Calls back while a declared element is **moving**.
   *
   * A mutation is an attribute written and a resize is a box changing size. An element that travels
   * without changing size — a pane sliding to where a closing sidebar used to be, animated by the
   * engine rather than by written styles — is neither, and a surface whose rectangle is only
   * recollected on those two events stands still while its pane leaves. Measured 2026-08-17 in the
   * named three-pane window: the pane went 165 → 584 over 190ms and the page stayed at 165 for the
   * whole of it, then appeared at the far end in one frame, 420 points from where it had been.
   */
  observeMoves(declarations: Iterable<NativeSurfaceDeclaration>, callback: () => void): () => void;
  schedule(callback: () => void): void;
};

export type NativeSurfaceObserverController = {
  stop(): void;
  status(): { sequence: number; committedSequence: number; running: boolean; dirty: boolean; error: unknown };
};

// The controller has exactly one serialized writer. Events that arrive during
// an in-flight commit collapse into one later full-inventory snapshot; no
// element can race an independent native write.
export function startNativeSurfaceObserver(
  runtime: NativeSurfaceObserverRuntime,
  commit: NativeSurfaceCommit,
  // The window this document is. One observer watches one document and every surface it finds
  // belongs to that window; the application names it, because a package that reads the DOM cannot
  // know which window the DOM is in.
  window: string,
  // Where the sequence resumes. A backend that has already accepted commits refuses anything at or
  // below what it holds, so an observer replacing an earlier one has to carry that number across.
  // Starting from zero again makes every commit stale and the screen freezes at the last one that
  // landed, with the refusals visible only to whoever reads the receipt.
  sequenceFloor = 0,
): NativeSurfaceObserverController {
  let stopped = false;
  let scheduled = false;
  let running = false;
  let dirty = false;
  let sequence = sequenceFloor;
  let committedSequence = 0;
  let error: unknown = null;
  let stopResize: () => void = () => {};
  let stopMove: () => void = () => {};

  const schedule = () => {
    if (stopped) return;
    dirty = true;
    if (scheduled || running) return;
    scheduled = true;
    runtime.schedule(() => { void flush(); });
  };
  const refreshResizeOwner = () => {
    stopResize();
    stopMove();
    const declarations = runtime.declarations();
    stopResize = runtime.observeResizes(declarations, schedule);
    stopMove = runtime.observeMoves(declarations, schedule);
  };
  const flush = async () => {
    scheduled = false;
    if (stopped || running || !dirty) return;
    dirty = false;
    running = true;
    const declarations = runtime.declarations();
    const snapshot = collectNativeSurfaceSnapshot(declarations, ++sequence, window);
    // What was declared, written back on the element that declared it. Without it the document can
    // be asked where a surface should be and the native layer where it is, and nothing can be asked
    // how far the declaration itself has fallen behind the element — the number that says the
    // commit is stale rather than the compositor wrong. Not in the attribute filter above, so
    // writing it schedules nothing.
    for (const declaration of declarations) {
      const surface = snapshot.surfaces.find((s) => s.id === declaration.dataset.nativeSurfaceId);
      if (!surface) continue;
      const frame = surface.frame;
      declaration.dataset.nativeDeclaredFrame = `${frame.x},${frame.y},${frame.width},${frame.height}`;
    }
    try {
      const receipt = await commit(snapshot);
      if (receipt.accepted && receipt.sequence === snapshot.sequence) {
        committedSequence = receipt.sequence;
        error = null;
      } else {
        error = new Error(`native surface commit rejected: requested=${snapshot.sequence} received=${receipt.sequence}`);
      }
    } catch (cause) {
      error = cause;
    } finally {
      running = false;
      if (dirty && !stopped) schedule();
    }
  };

  refreshResizeOwner();
  const stopMutation = runtime.observeMutations((mutation) => {
    if (stopped) return;
    if (mutation.inventoryChanged) refreshResizeOwner();
    schedule();
  });
  schedule();

  return {
    stop() {
      if (stopped) return;
      stopped = true;
      dirty = false;
      stopMutation();
      stopResize();
      stopMove();
    },
    status: () => ({ sequence, committedSequence, running, dirty, error }),
  };
}

/** What declares a native surface in a document. Published because a reader outside this package
 *  that wants the same elements would otherwise write the attribute name again, and the day it
 *  changes one of the two is quietly reading nothing. */
export const nativeSurfaceDeclarationSelector = "[data-native-surface]";
const declarationSelector = nativeSurfaceDeclarationSelector;

export function nativeSurfaceDOMRuntime(
  root: ParentNode = document,
  scheduleMicrotask: (callback: () => void) => void = queueMicrotask,
): NativeSurfaceObserverRuntime {
  return {
    declarations: () => Array.from(root.querySelectorAll<HTMLElement>(declarationSelector)),
    observeMutations(callback) {
      const hasDeclaration = (node: Node) => node instanceof Element
        && (node.matches(declarationSelector) || node.querySelector(declarationSelector) !== null);
      const observer = new MutationObserver((records) => {
        const relevant = records.filter((record) => {
          if (!(record.target instanceof Element)) return false;
          if (record.type === "attributes") {
            return record.target.matches(declarationSelector) || record.target.querySelector(declarationSelector) !== null;
          }
          return record.target.matches(declarationSelector)
            || record.target.querySelector(declarationSelector) !== null
            || Array.from(record.addedNodes).some(hasDeclaration)
            || Array.from(record.removedNodes).some(hasDeclaration);
        });
        if (relevant.length === 0) return;
        callback({
          inventoryChanged: relevant.some((record) => record.type === "childList"
            && (Array.from(record.addedNodes).some(hasDeclaration) || Array.from(record.removedNodes).some(hasDeclaration)))
            || relevant.some((record) => record.type === "attributes" && record.attributeName === "data-native-surface"),
        });
      });
      observer.observe(root, {
        subtree: true,
        childList: true,
        attributes: true,
        attributeFilter: [
          "class", "style", "hidden",
          "data-native-surface", "data-native-surface-id", "data-native-generation",
          "data-native-source", "data-native-visible", "data-native-alpha", "data-native-layer",
        ],
      });
      return () => observer.disconnect();
    },
    observeResizes(declarations, callback) {
      const observer = new ResizeObserver(callback);
      for (const declaration of declarations) observer.observe(declaration as unknown as Element);
      return () => observer.disconnect();
    },
    // A move has no event. The engine animates a pane's position without writing a style and
    // without changing its size, so the only way to know a declared element has travelled is to
    // measure it — once per frame, over the surfaces this window declares, which is a handful of
    // rectangles. The callback fires only when one of them actually differs, so a still window
    // commits nothing.
    observeMoves(declarations, callback) {
      const watched = Array.from(declarations);
      if (watched.length === 0) return () => {};
      const view = (root as Document).defaultView ?? globalThis;
      const frame = view.requestAnimationFrame?.bind(view);
      const cancel = view.cancelAnimationFrame?.bind(view);
      if (!frame) return () => {};
      const last = new Map<NativeSurfaceDeclaration, string>();
      const placeOf = (declaration: NativeSurfaceDeclaration): string => {
        const rect = declaration.getBoundingClientRect();
        return `${rect.left},${rect.top},${rect.width},${rect.height}`;
      };
      for (const declaration of watched) last.set(declaration, placeOf(declaration));
      let stopped = false;
      let pending = 0;
      const tick = () => {
        if (stopped) return;
        let moved = false;
        for (const declaration of watched) {
          if (!declaration.isConnected) continue;
          const place = placeOf(declaration);
          if (last.get(declaration) !== place) {
            last.set(declaration, place);
            moved = true;
          }
        }
        if (moved) callback();
        pending = frame(tick);
      };
      pending = frame(tick);
      return () => {
        stopped = true;
        cancel?.(pending);
      };
    },
    schedule: (callback) => scheduleMicrotask(callback),
  };
}
