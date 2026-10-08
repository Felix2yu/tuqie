import type { Naming } from './lib/cuts';
import { sanitizePrefix } from './lib/cuts';
import type { Analysis, Axis, Format } from './types';

async function errorMessage(res: Response): Promise<string> {
  try {
    const body = await res.json();
    if (body && typeof body.error === 'string') return body.error;
  } catch {
    /* not JSON */
  }
  return `请求失败（${res.status}）`;
}

export async function analyze(file: File): Promise<Analysis> {
  const form = new FormData();
  form.append('file', file);
  // Screenshots usually carry no capture date, so the file's own timestamp is sent
  // along as the fallback.
  if (file.lastModified > 0) form.append('lastModified', String(file.lastModified));
  const res = await fetch('/api/analyze', { method: 'POST', body: form });
  if (!res.ok) throw new Error(await errorMessage(res));
  return (await res.json()) as Analysis;
}

export function sliceUrl(
  id: string,
  axis: Axis,
  from: number,
  to: number,
  index: number,
  format: Format,
  quality: number,
): string {
  const params = new URLSearchParams({
    id,
    axis,
    from: String(from),
    to: String(to),
    index: String(index),
    format,
    quality: String(quality),
  });
  return `/api/slice?${params.toString()}`;
}

export async function fetchSlice(
  id: string,
  axis: Axis,
  from: number,
  to: number,
  index: number,
  format: Format,
  quality: number,
): Promise<Blob> {
  const res = await fetch(sliceUrl(id, axis, from, to, index, format, quality));
  if (!res.ok) throw new Error(await errorMessage(res));
  return res.blob();
}

/**
 * How many files the archive the server actually wrote holds, read from its
 * end-of-central-directory record. The export streams, so a band that fails to
 * encode is left out and the response is still a 200; the count is what gives the
 * browser a way to notice. Null means the record is not there at all, which is a
 * truncated download.
 */
export async function zipCount(blob: Blob): Promise<number | null> {
  const tail = await blob.slice(Math.max(0, blob.size - 66000)).arrayBuffer();
  const view = new DataView(tail);
  for (let at = tail.byteLength - 22; at >= 0; at--) {
    if (view.getUint32(at, true) !== 0x06054b50) continue;
    return view.getUint16(at + 10, true);
  }
  return null;
}

export async function fetchZip(
  id: string,
  axis: Axis,
  cuts: number[],
  skip: number[],
  naming: Naming,
  format: Format,
  quality: number,
): Promise<Blob> {
  const res = await fetch('/api/export', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({
      id,
      axis,
      cuts,
      skip,
      prefix: sanitizePrefix(naming.prefix),
      start: naming.start,
      format,
      quality,
    }),
  });
  if (!res.ok) throw new Error(await errorMessage(res));
  return res.blob();
}
