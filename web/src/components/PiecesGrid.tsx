import { bandLabel, type Band } from '../lib/cuts';
import type { Axis } from '../types';

type Props = {
  src: string;
  axis: Axis;
  imageWidth: number;
  imageHeight: number;
  bands: Band[];
  /** Bands left out of the export, by their id; the rest are numbered without them. */
  skipped: Set<string>;
  /** The fixed thumbnail dimension: width for rows, height for columns. */
  thumbSize?: number;
  onToggleSkip: (id: string) => void;
  onPick: (band: Band) => void;
};

export default function PiecesGrid({
  src,
  axis,
  imageWidth,
  imageHeight,
  bands,
  skipped,
  thumbSize = 92,
  onToggleSkip,
  onPick,
}: Props) {
  const vertical = axis !== 'x';
  // Rows are cut across the full width, so a strip's thumbnail is as wide as the
  // picture is narrow; columns get the mirror of that.
  const scale = thumbSize / (vertical ? imageWidth : imageHeight);

  // Export numbers run over the kept pieces only, so excluding one closes the gap
  // instead of leaving a hole in the sequence.
  const ordinals = new Map<string, number>();
  for (const b of bands) if (!skipped.has(b.id)) ordinals.set(b.id, ordinals.size);

  return (
    <div className="flex gap-2 overflow-x-auto pb-1">
      {bands.map((b) => {
        const box = Math.max(14, Math.min(200, b.size * scale));
        const out = skipped.has(b.id);
        const ordinal = ordinals.get(b.id);
        return (
          <figure key={b.id} className="shrink-0">
            <div className="relative">
              <button
                type="button"
                title="点击放大这一张"
                aria-label={`放大第 ${b.index + 1} 张，${bandLabel(axis, imageWidth, imageHeight, b)}`}
                onClick={() => onPick(b)}
                className={`block cursor-zoom-in overflow-hidden rounded border border-ink-700 bg-ink-800 transition hover:border-accent focus-visible:border-accent focus-visible:outline-none ${
                  out ? 'opacity-30 grayscale' : ''
                }`}
                style={{
                  width: vertical ? thumbSize : box,
                  height: vertical ? box : thumbSize,
                  backgroundImage: `url(${src})`,
                  backgroundSize: `${imageWidth * scale}px ${imageHeight * scale}px`,
                  backgroundPosition: vertical ? `0 -${b.from * scale}px` : `-${b.from * scale}px 0`,
                }}
              />
              <button
                type="button"
                aria-pressed={out}
                title={out ? '这张重新计入导出' : '导出时跳过这一张'}
                onClick={() => onToggleSkip(b.id)}
                className={`absolute right-1 top-1 grid h-5 w-5 place-content-center rounded text-[11px] ${
                  out ? 'bg-rose-500/80 text-white' : 'bg-ink-900/85 text-ink-400 hover:text-slate-200'
                }`}
              >
                {out ? '↺' : '−'}
              </button>
            </div>
            <figcaption
              className={`mt-1 text-center text-[10px] tabular-nums ${out ? 'text-rose-400' : 'text-ink-400'}`}
            >
              {out ? '跳过' : `${(ordinal ?? 0) + 1} · `}
              {bandLabel(axis, imageWidth, imageHeight, b)}
            </figcaption>
          </figure>
        );
      })}
    </div>
  );
}
