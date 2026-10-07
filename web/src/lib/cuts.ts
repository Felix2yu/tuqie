import type { Axis, Candidate, Format } from '../types';

// A piece thinner than this is almost certainly a stray line, not a photo.
export const MIN_PIECE_PX = 30;

export type Band = { index: number; from: number; to: number; size: number };

/** How far cut positions run: the width for a left-to-right stitch, the height otherwise. */
export function axisLength(axis: Axis, width: number, height: number): number {
  return axis === 'x' ? width : height;
}

export function normalize(cuts: number[], length: number): number[] {
  const sorted = cuts
    .map((p) => Math.round(p))
    .filter((p) => p >= MIN_PIECE_PX && p <= length - MIN_PIECE_PX)
    .sort((a, b) => a - b);
  const out: number[] = [];
  for (const p of sorted) {
    if (out.length > 0 && p - out[out.length - 1] < MIN_PIECE_PX) continue;
    out.push(p);
  }
  return out;
}

/**
 * Detected lines are kept as long as their score passes the threshold and the
 * user has not removed them; hand-drawn lines always survive. Raising the
 * threshold therefore re-adds lines without throwing away manual work.
 */
export function resolveCuts(
  candidates: Candidate[],
  threshold: number,
  removed: number[],
  manual: number[],
  length: number,
): number[] {
  const gone = new Set(removed);
  const auto = candidates.filter((c) => c.score >= threshold && !gone.has(c.pos)).map((c) => c.pos);
  return normalize([...auto, ...manual.filter((p) => !gone.has(p))], length);
}

export function bandsFromCuts(cuts: number[], length: number): Band[] {
  if (cuts.length === 0) return [{ index: 0, from: 0, to: length, size: length }];
  const bands: Band[] = [];
  let prev = 0;
  cuts.forEach((p, i) => {
    bands.push({ index: i, from: prev, to: p, size: p - prev });
    prev = p;
  });
  bands.push({ index: cuts.length, from: prev, to: length, size: length - prev });
  return bands;
}

/** Keeps a dragged line from sliding over its neighbours. */
export function clampCut(pos: number, cuts: number[], index: number, length: number): number {
  const lo = index === 0 ? MIN_PIECE_PX : cuts[index - 1] + MIN_PIECE_PX;
  const hi = index === cuts.length - 1 ? length - MIN_PIECE_PX : cuts[index + 1] - MIN_PIECE_PX;
  return Math.min(Math.max(Math.round(pos), Math.min(lo, hi)), Math.max(lo, hi));
}

export function positionOf(pos: number, length: number): number {
  return length <= 0 ? 0 : (pos / length) * 100;
}

export function pieceName(base: string, index: number, ext: string): string {
  const stem = base.replace(/\.[^./]*$/, '') || 'screenshot';
  return `${stem}-${String(index + 1).padStart(2, '0')}.${ext}`;
}

/** The size of a band as it appears under the thumbnail and in the lightbox. */
export function bandLabel(axis: Axis, width: number, height: number, band: Band): string {
  return axis === 'x' ? `${band.size}×${height}` : `${width}×${band.size}`;
}

/** The extension the server names a slice with. */
export function extFor(format: Format): string {
  return format === 'png' ? 'png' : 'jpg';
}

export function formatBytes(n: number): string {
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(0)} KB`;
  return `${(n / 1024 / 1024).toFixed(1)} MB`;
}
