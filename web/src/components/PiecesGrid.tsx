import { bandLabel, type Band } from '../lib/cuts';
import type { Axis } from '../types';

type Props = {
  src: string;
  axis: Axis;
  imageWidth: number;
  imageHeight: number;
  bands: Band[];
  /** The fixed thumbnail dimension: width for rows, height for columns. */
  thumbSize?: number;
  onPick: (band: Band) => void;
};

export default function PiecesGrid({
  src,
  axis,
  imageWidth,
  imageHeight,
  bands,
  thumbSize = 92,
  onPick,
}: Props) {
  const vertical = axis !== 'x';
  // Rows are cut across the full width, so a strip's thumbnail is as wide as the
  // picture is narrow; columns get the mirror of that.
  const scale = thumbSize / (vertical ? imageWidth : imageHeight);
  return (
    <div className="flex gap-2 overflow-x-auto pb-1">
      {bands.map((b) => {
        const box = Math.max(14, Math.min(200, b.size * scale));
        return (
          <figure key={b.index} className="shrink-0">
            <button
              type="button"
              title="点击放大这一张"
              aria-label={`放大第 ${b.index + 1} 张，${bandLabel(axis, imageWidth, imageHeight, b)}`}
              onClick={() => onPick(b)}
              className="block cursor-zoom-in overflow-hidden rounded border border-ink-700 bg-ink-800 transition hover:border-accent focus-visible:border-accent focus-visible:outline-none"
              style={{
                width: vertical ? thumbSize : box,
                height: vertical ? box : thumbSize,
                backgroundImage: `url(${src})`,
                backgroundSize: `${imageWidth * scale}px ${imageHeight * scale}px`,
                backgroundPosition: vertical ? `0 -${b.from * scale}px` : `-${b.from * scale}px 0`,
              }}
            />
            <figcaption className="mt-1 text-center text-[10px] tabular-nums text-ink-400">
              {b.index + 1} · {bandLabel(axis, imageWidth, imageHeight, b)}
            </figcaption>
          </figure>
        );
      })}
    </div>
  );
}
