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

  const schedule = () => {
    if (stopped) return;
    dirty = true;
    if (scheduled || running) return;
    scheduled = true;
    runtime.schedule(() => { void flush(); });
  };
  const refreshResizeOwner = () => {
    stopResize();
    stopResize = runtime.observeResizes(runtime.declarations(), schedule);
  };
  const flush = async () => {
    scheduled = false;
    if (stopped || running || !dirty) return;
    dirty = false;
    running = true;
    const snapshot = collectNativeSurfaceSnapshot(runtime.declarations(), ++sequence, window);
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
    },
    status: () => ({ sequence, committedSequence, running, dirty, error }),
  };
}

const declarationSelector = "[data-native-surface]";

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
    schedule: (callback) => scheduleMicrotask(callback),
  };
}
