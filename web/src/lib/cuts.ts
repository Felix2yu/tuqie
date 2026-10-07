import type { Axis, Candidate, Format } from '../types';

// A piece thinner than this is almost certainly a stray line, not a photo.
export const MIN_PIECE_PX = 30;

export type Band = { index: number; from: number; to: number; size: number };

/** How a regular grid of cuts is asked for: a number of pieces, or a piece size. */
export type SplitMode = 'count' | 'length';

/** The smallest sensible value for each mode; anything below is a sliver, not a piece. */
export function splitFloor(mode: SplitMode): number {
  return mode === 'count' ? 2 : MIN_PIECE_PX;
}

/** The cut set a split request stands for, in either mode. */
export function splitCuts(mode: SplitMode, value: number, length: number): number[] {
  return mode === 'count' ? equalCuts(length, value) : fixedCuts(length, value);
}

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
 *
 * The slider's top position means "no detected line at all". Scores can reach 1
 * exactly, so the comparison alone would leave the strongest line behind.
 */
export function resolveCuts(
  candidates: Candidate[],
  threshold: number,
  removed: number[],
  manual: number[],
  length: number,
): number[] {
  const gone = new Set(removed);
  const auto =
    threshold >= 1
      ? []
      : candidates.filter((c) => c.score >= threshold && !gone.has(c.pos)).map((c) => c.pos);
  return normalize([...auto, ...manual.filter((p) => !gone.has(p))], length);
}

/** N equal bands need N-1 interior lines. Rounding each one on its own keeps the
 *  ends square instead of letting the error drift across the picture. */
export function equalCuts(length: number, count: number): number[] {
  if (count < 2) return [];
  const out: number[] = [];
  for (let i = 1; i < count; i++) out.push(Math.round((length * i) / count));
  return normalize(out, length);
}

/** Lines every step pixels. Whatever is left at the far end becomes its own band,
 *  unless it is too thin to be a piece, in which case it joins the last one. */
export function fixedCuts(length: number, step: number): number[] {
  if (step < MIN_PIECE_PX) return [];
  const out: number[] = [];
  for (let p = step; p < length; p += step) out.push(p);
  return normalize(out, length);
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

/**
 * What a drag that ended on `pos` means for the two lists the lines are built
 * from. The detected line the user moved away from has to stay out of the way,
 * and the position the line lands on has to stop being one they deleted —
 * otherwise dragging one line onto the spot another one came from loses both.
 * Returns null when the line did not actually move.
 */
export function applyMove(
  cuts: number[],
  index: number,
  pos: number,
  length: number,
  manual: number[],
  removed: number[],
): { manual: number[]; removed: number[] } | null {
  const old = cuts[index];
  if (old === undefined) return null;
  const next = clampCut(pos, cuts, index, length);
  if (next === old) return null;
  // A position the user invented is simply left behind. Only a detected line gets
  // banned, and banning it by position is what keeps the threshold from putting it
  // back the moment the drag ends.
  const leftIsManual = manual.includes(old);
  const kept = removed.filter((p) => p !== next);
  // Landing on a deleted detection brings that one back, so the dragged line does
  // not also need its own entry there — writing both would show two lines where
  // the user sees one, and the next drag would leave the resurrected one behind.
  const resurrects = kept.length !== removed.length;
  return {
    manual: normalize(
      [...manual.filter((p) => p !== old && p !== next), ...(resurrects ? [] : [next])],
      length,
    ),
    removed: !leftIsManual && !kept.includes(old) ? [...kept, old] : kept,
  };
}

export function positionOf(pos: number, length: number): number {
  return length <= 0 ? 0 : (pos / length) * 100;
}

/** What a slice is called: a prefix the user typed and the number of that piece. */
export type Naming = { prefix: string; start: number };

/** The file stem an upload brings, which is the prefix a new session starts with. */
export function stemOf(filename: string): string {
  return filename.replace(/\.[^./]*$/, '') || 'screenshot';
}

/**
 * A prefix becomes part of a zip entry name, and extractors follow "../" without
 * complaining, so separators and control characters are flattened out here. The
 * server applies the same rule to whatever arrives over the wire.
 */
export function sanitizePrefix(raw: string): string {
  const s = raw
    .trim()
    .replace(/[\/\\:"\u0000-\u001f]/g, '_')
    .replace(/^[. ]+|[. ]+$/g, '')
    .replace(/\.\./g, '_');
  const capped = Array.from(s).slice(0, 80).join('');
  return capped.replace(/[. ]+$/, '');
}

/** Two digits at minimum, wider once the run outgrows that, so files sort by name. */
export function namePad(start: number, count: number): number {
  const last = Math.max(start, start + count - 1);
  return Math.max(2, String(last).length);
}

export function pieceName(naming: Naming, ordinal: number, ext: string, pad: number): string {
  const num = String(naming.start + ordinal).padStart(pad, '0');
  return `${sanitizePrefix(naming.prefix) || 'screenshot'}-${num}.${ext}`;
}

/** The size of a band as it appears under the thumbnail and in the lightbox. */
export function bandLabel(axis: Axis, width: number, height: number, band: Band): string {
  return axis === 'x' ? `${band.size}×${height}` : `${width}×${band.size}`;
}

const EXT: Record<Format, string> = { jpeg: 'jpg', png: 'png', heic: 'heic', avif: 'avif', jxl: 'jxl' };
const MIME: Record<Format, string> = {
  jpeg: 'image/jpeg',
  png: 'image/png',
  heic: 'image/heic',
  avif: 'image/avif',
  jxl: 'image/jxl',
};

/** The extension the server names a slice with. */
export function extFor(format: Format): string {
  return EXT[format];
}

/** The type a shared File has to carry for the receiving app to recognise it. */
export function mimeFor(format: Format): string {
  return MIME[format];
}

export function formatBytes(n: number): string {
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(0)} KB`;
  return `${(n / 1024 / 1024).toFixed(1)} MB`;
}
