export type NativeSurfaceFrame = { x: number; y: number; width: number; height: number };
export type NativeSurface = {
  id: string;
  generation: number;
  kind: "browser";
  frame: NativeSurfaceFrame;
  visible: boolean;
  alpha: number;
  layer: number;
  source: { url: string };
};
export type NativeSurfaceSnapshot = { sequence: number; surfaces: NativeSurface[] };

export type NativeSurfaceDeclaration = {
  dataset: DOMStringMap | Record<string, string | undefined>;
  isConnected: boolean;
  getBoundingClientRect(): Pick<DOMRect, "left" | "top" | "width" | "height">;
};

export function collectNativeSurfaceSnapshot(
  declarations: Iterable<NativeSurfaceDeclaration>,
  sequence: number,
): NativeSurfaceSnapshot {
  if (!Number.isSafeInteger(sequence) || sequence <= 0) {
    throw new Error(`native surface snapshot sequence is invalid: ${sequence}`);
  }
  const seen = new Set<string>();
  const surfaces: NativeSurface[] = [];

  for (const declaration of declarations) {
    const kind = declaration.dataset.wailsNativeSurface;
    const id = declaration.dataset.nativeSurfaceId ?? "";
    const generation = Number(declaration.dataset.nativeGeneration);
    const layer = Number(declaration.dataset.nativeLayer ?? 0);
    const alpha = Number(declaration.dataset.nativeAlpha ?? 1);
    if (kind !== "browser") throw new Error(`unsupported native surface kind: ${kind ?? "missing"}`);
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
      source: { url: declaration.dataset.nativeUrl ?? "about:blank" },
    });
  }

  surfaces.sort((left, right) => left.id.localeCompare(right.id));
  return { sequence, surfaces };
}
