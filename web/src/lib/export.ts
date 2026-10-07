import { fetchSlice, fetchZip } from '../api';
import type { Band } from './cuts';
import { pieceName } from './cuts';
import type { Axis, Format } from '../types';

export type Piece = { name: string; type: string; blob: Blob; band: Band };

export type Progress = (done: number, total: number) => void;

export function isIOS(): boolean {
  if (typeof navigator === 'undefined') return false;
  const ua = navigator.userAgent;
  const ipadOS = /Macintosh/.test(ua) && navigator.maxTouchPoints > 1;
  return /iPhone|iPad|iPod/.test(ua) || ipadOS;
}

/**
 * The only web route into the Photos library is the system share sheet, where
 * the user taps "存储图像". Availability has to be probed with a real File.
 */
export function canShareFiles(): boolean {
  if (typeof navigator === 'undefined' || !navigator.canShare) return false;
  try {
    const probe = new File([new Blob([''], { type: 'image/png' })], 'probe.png', { type: 'image/png' });
    return navigator.canShare({ files: [probe] });
  } catch {
    return false;
  }
}

export async function renderPieces(
  id: string,
  axis: Axis,
  bands: Band[],
  base: string,
  format: Format,
  quality: number,
  onProgress?: Progress,
): Promise<Piece[]> {
  const ext = format === 'png' ? 'png' : 'jpg';
  const type = format === 'png' ? 'image/png' : 'image/jpeg';
  const pieces: Piece[] = [];
  for (const band of bands) {
    const blob = await fetchSlice(id, axis, band.from, band.to, band.index, format, quality);
    pieces.push({ name: pieceName(base, band.index, ext), blob, type, band });
    onProgress?.(pieces.length, bands.length);
  }
  return pieces;
}

export type ShareOutcome = 'shared' | 'cancelled' | 'failed';

export async function sharePieces(pieces: Piece[], taken?: number): Promise<ShareOutcome> {
  // A shared File with no timestamp of its own is filed under the moment it was
  // saved, so the capture date travels with it instead.
  const files = pieces.map((p) =>
    new File([p.blob], p.name, taken ? { type: p.type, lastModified: taken } : { type: p.type }),
  );
  try {
    await navigator.share({ files, title: '图切' });
    return 'shared';
  } catch (err) {
    const name = (err as DOMException)?.name;
    if (name === 'AbortError') return 'cancelled';
    console.warn('share failed', err);
    return 'failed';
  }
}

export async function zipPieces(
  id: string,
  axis: Axis,
  cuts: number[],
  format: Format,
  quality: number,
): Promise<Blob> {
  return fetchZip(id, axis, cuts, format, quality);
}

export function downloadBlob(blob: Blob, name: string): void {
  const url = URL.createObjectURL(blob);
  const a = document.createElement('a');
  a.href = url;
  a.download = name;
  document.body.appendChild(a);
  a.click();
  a.remove();
  // iOS Safari keeps the blob alive only briefly after the tap, but revoking too
  // early cancels the download on desktop.
  setTimeout(() => URL.revokeObjectURL(url), 60_000);
}
