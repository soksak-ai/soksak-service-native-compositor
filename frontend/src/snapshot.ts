export type NativeSurfaceFrame = { x: number; y: number; width: number; height: number };
export type NativeSurface = {
  id: string;
  generation: number;
  kind: string;
  frame: NativeSurfaceFrame;
  visible: boolean;
  alpha: number;
  layer: number;
  source: Record<string, string>;
};
/**
 * One window's complete declared inventory.
 *
 * `window` names the window whose document declared these surfaces. A surface is attached to that
 * window's content view and its frame is in that document's coordinates, so a snapshot without the
 * name leaves the host to resolve whichever window it happens to hold. Measured 2026-08-16: a
 * workspace window's browser was created inside the orchestrator — 1128×718 inside a 999×617
 * window — and declared-versus-applied still read zero drift, because both halves came back from
 * the same wrong window.
 */
export type NativeSurfaceSnapshot = {
  window: string;
  sequence: number;
  /** True from the layout system's begin edge through its matching end edge. The native owner
   *  uses this fact to choose interactive presentation without inferring policy from timing. */
  interactive: boolean;
  surfaces: NativeSurface[];
  /** When this document handed the commit over, by the wall clock. The receipt subtracts it
   *  to say how long the crossing took — a round trip with nothing but the backend's own
   *  time in it leaves the rest unaccounted for, and that rest was 39.8 of 40ms. */
  sentAtUnixMs: number;
};

export type NativeSurfaceDeclaration = {
  dataset: DOMStringMap | Record<string, string | undefined>;
  isConnected: boolean;
  getBoundingClientRect(): Pick<DOMRect, "left" | "top" | "width" | "height">;
};

export function collectNativeSurfaceSnapshot(
  declarations: Iterable<NativeSurfaceDeclaration>,
  sequence: number,
  window: string,
  interactive = false,
): NativeSurfaceSnapshot {
  // Refused, never defaulted. The Go half refuses it too, and a default here would answer that
  // refusal with a name the document did not choose.
  if (window === "") {
    throw new Error("native surface snapshot names no window");
  }
  if (!Number.isSafeInteger(sequence) || sequence <= 0) {
    throw new Error(`native surface snapshot sequence is invalid: ${sequence}`);
  }
  const seen = new Set<string>();
  const surfaces: NativeSurface[] = [];

  for (const declaration of declarations) {
    const kind = declaration.dataset.nativeSurface;
    const id = declaration.dataset.nativeSurfaceId ?? "";
    const generation = Number(declaration.dataset.nativeGeneration);
    const layer = Number(declaration.dataset.nativeLayer ?? 0);
    const alpha = Number(declaration.dataset.nativeAlpha ?? 1);
    let source: Record<string, string>;
    try {
      source = JSON.parse(declaration.dataset.nativeSource ?? "{}") as Record<string, string>;
    } catch {
      throw new Error(`native surface source is invalid: ${id || "missing"}`);
    }
    if (source === null || Array.isArray(source) || Object.values(source).some((value) => typeof value !== "string")) {
      throw new Error(`native surface source is invalid: ${id || "missing"}`);
    }
    if (!kind) throw new Error("native surface kind is required");
    if (!id) throw new Error("native surface id is required");
    if (seen.has(id)) throw new Error(`duplicate native surface id: ${id}`);
    if (!Number.isSafeInteger(generation) || generation <= 0) {
      throw new Error(`native surface generation is invalid: ${id}/${declaration.dataset.nativeGeneration ?? "missing"}`);
    }
    if (!Number.isSafeInteger(layer) || !Number.isFinite(alpha) || alpha < 0 || alpha > 1) {
      throw new Error(`native surface presentation is invalid: ${id}`);
    }
    seen.add(id);
    const rect = declaration.getBoundingClientRect();
    const frame = { x: rect.left, y: rect.top, width: rect.width, height: rect.height };
    if (Object.values(frame).some((value) => !Number.isFinite(value) || value < 0)) {
      throw new Error(`native surface frame is invalid: ${id}`);
    }
    surfaces.push({
      id,
      generation,
      kind,
      frame,
      visible: declaration.isConnected && declaration.dataset.nativeVisible !== "false" && frame.width > 0 && frame.height > 0,
      alpha,
      layer,
      source,
    });
  }

  surfaces.sort((left, right) => left.id.localeCompare(right.id));
  return { window, sequence, interactive, surfaces, sentAtUnixMs: Date.now() };
}
