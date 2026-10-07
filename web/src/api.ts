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

export async function fetchZip(
  id: string,
  axis: Axis,
  cuts: number[],
  format: Format,
  quality: number,
): Promise<Blob> {
  const res = await fetch('/api/export', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ id, axis, cuts, format, quality }),
  });
  if (!res.ok) throw new Error(await errorMessage(res));
  return res.blob();
}
