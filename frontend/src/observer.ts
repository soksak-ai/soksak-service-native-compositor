import { collectNativeSurfaceSnapshot, type NativeSurfaceDeclaration, type NativeSurfaceSnapshot } from "./snapshot";

export type NativeSurfaceReceipt = {
  sequence: number;
  accepted: boolean;
  surfaces: ReadonlyArray<unknown>;
};

export type NativeSurfaceCommit = (snapshot: NativeSurfaceSnapshot) => Promise<NativeSurfaceReceipt>;
export type NativeSurfaceObserverRuntime = {
  declarations(): Iterable<NativeSurfaceDeclaration>;
  observeMutations(callback: () => void): () => void;
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
): NativeSurfaceObserverController {
  let stopped = false;
  let scheduled = false;
  let running = false;
  let dirty = false;
  let sequence = 0;
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
    const snapshot = collectNativeSurfaceSnapshot(runtime.declarations(), ++sequence);
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
  const stopMutation = runtime.observeMutations(() => {
    if (stopped) return;
    refreshResizeOwner();
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

const declarationSelector = "[data-wails-native-surface]";

export function nativeSurfaceDOMRuntime(
  root: ParentNode = document,
  scheduleMicrotask: (callback: () => void) => void = queueMicrotask,
): NativeSurfaceObserverRuntime {
  return {
    declarations: () => Array.from(root.querySelectorAll<HTMLElement>(declarationSelector)),
    observeMutations(callback) {
      const observer = new MutationObserver(callback);
      observer.observe(root, {
        subtree: true,
        childList: true,
        attributes: true,
        attributeFilter: [
          "class", "style", "hidden",
          "data-wails-native-surface", "data-native-surface-id", "data-native-generation",
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
