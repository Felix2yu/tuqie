import { useCallback, useEffect, useRef, useState } from 'react';
import { axisLength, positionOf, type Cut } from '../lib/cuts';
import type { Axis } from '../types';

type Props = {
  src: string;
  axis: Axis;
  imageWidth: number;
  imageHeight: number;
  cuts: Cut[];
  fit: 'fill' | 'page';
  onAdd: (pos: number) => void;
  onMove: (id: string, pos: number) => void;
  /** The released position of a drag, or a typed one: the only moment a move is kept. */
  onCommit: (id: string, pos: number) => void;
  onRemove: (id: string) => void;
  onViewport?: (window: [number, number]) => void;
};

export default function Stage({
  src,
  axis,
  imageWidth,
  imageHeight,
  cuts,
  fit,
  onAdd,
  onMove,
  onCommit,
  onRemove,
  onViewport,
}: Props) {
  const img = useRef<HTMLImageElement>(null);
  const scroller = useRef<HTMLDivElement>(null);
  const [drag, setDrag] = useState<string | null>(null);
  const dragTo = useRef<number | null>(null);
  const [pageBox, setPageBox] = useState<{ w: number; h: number } | null>(null);
  const [edit, setEdit] = useState<{ id: string; text: string } | null>(null);

  // A horizontal stitch is measured across the width and cut by vertical lines.
  const across = axis === 'x';
  const length = axisLength(axis, imageWidth, imageHeight);

  // The cut overlay is placed in percentages of the image box, so the box is
  // sized from the measured container rather than left to CSS: an img element
  // carries a default max-width, and letting it clamp would squash a wide
  // screenshot into a box that no longer matches the pixels it shows.
  const measure = useCallback(() => {
    const el = img.current;
    const box = scroller.current;
    if (!el || !box) return;
    const natW = el.naturalWidth;
    const natH = el.naturalHeight;
    if (!natW || !natH) return;
    const cw = box.clientWidth;
    const ch = box.clientHeight;
    if (fit === 'page') {
      const s = Math.min(cw / natW, ch / natH);
      setPageBox({ w: Math.max(1, Math.round(natW * s)), h: Math.max(1, Math.round(natH * s)) });
      return;
    }
    // Fill mode scrolls along the long side: down for a tall screenshot,
    // sideways for a wide one.
    setPageBox(across ? { w: Math.max(1, Math.round(ch * (natW / natH))), h: ch } : { w: cw, h: Math.max(1, Math.round(cw * (natH / natW))) });
  }, [fit, across]);

  useEffect(() => {
    measure();
    const box = scroller.current;
    if (!box) return;
    const ro = new ResizeObserver(measure);
    ro.observe(box);
    return () => ro.disconnect();
  }, [measure]);

  const reportViewport = useCallback(() => {
    const el = img.current;
    const box = scroller.current;
    if (!el || !box || !onViewport) return;
    const r = el.getBoundingClientRect();
    const b = box.getBoundingClientRect();
    const span = across ? r.width : r.height;
    if (span <= 0) return;
    const clamp01 = (v: number) => Math.min(Math.max(v, 0), 1);
    const start = (across ? b.left - r.left : b.top - r.top) / span;
    const end = (across ? b.right - r.left : b.bottom - r.top) / span;
    onViewport([clamp01(start), clamp01(end)]);
  }, [onViewport, across]);

  const posAt = useCallback(
    (clientX: number, clientY: number) => {
      const el = img.current;
      if (!el) return 0;
      const rect = el.getBoundingClientRect();
      const span = across ? rect.width : rect.height;
      if (span <= 0) return 0;
      const ratio = (across ? clientX - rect.left : clientY - rect.top) / span;
      return Math.round(Math.min(Math.max(ratio, 0), 1) * length);
    },
    [across, length],
  );

  const addAtCentre = () => {
    const el = img.current;
    if (!el) return;
    const rect = el.getBoundingClientRect();
    onAdd(posAt(rect.left + rect.width / 2, rect.top + rect.height / 2));
  };

  // The chip is a text field rather than type=number: a wheel over the stage would
  // otherwise spin the focused value, and "1200px" is a natural thing to paste.
  const commitEdit = () => {
    if (!edit) return;
    const digits = edit.text.match(/\d+/);
    const id = edit.id;
    setEdit(null);
    if (digits) onCommit(id, Number(digits[0]));
  };

  // Capture can fail on a pointer the browser already released, and then a finger
  // lifted outside the strip would leave the previewed line uncommitted.
  useEffect(() => {
    if (drag === null) return;
    const end = () => {
      if (dragTo.current !== null) onCommit(drag, dragTo.current);
      dragTo.current = null;
      setDrag(null);
    };
    window.addEventListener('pointerup', end);
    window.addEventListener('pointercancel', end);
    return () => {
      window.removeEventListener('pointerup', end);
      window.removeEventListener('pointercancel', end);
    };
  }, [drag, onCommit]);

  // A pointer that has already been released makes setPointerCapture throw.
  const capture = (e: React.PointerEvent) => {
    try {
      e.currentTarget.setPointerCapture(e.pointerId);
    } catch {
      /* fall back to the events this element still receives */
    }
  };

  return (
    <div className={`relative min-h-0 flex-1 ${drag !== null ? 'no-select' : ''}`}>
      <div ref={scroller} className="thin-scroll h-full overflow-auto" onScroll={reportViewport}>
        <div className={fit === 'page' ? 'flex h-full items-center justify-center' : ''}>
          <div className="relative" style={pageBox ? { width: pageBox.w, height: pageBox.h } : undefined}>
            <img
              ref={img}
              src={src}
              alt="待拆分的长截图"
              draggable={false}
              className="block h-full w-full select-none"
              onLoad={() => {
                measure();
                reportViewport();
              }}
              onDoubleClick={(e) => onAdd(posAt(e.clientX, e.clientY))}
            />

            {cuts.map((cut, i) => (
              <div
                key={cut.id}
                className={
                  across
                    ? 'grab absolute inset-y-0 -ml-[17px] w-[34px] cursor-ew-resize'
                    : 'grab absolute inset-x-0 -mt-[17px] h-[34px] cursor-ns-resize'
                }
                style={across ? { left: `${positionOf(cut.pos, length)}%` } : { top: `${positionOf(cut.pos, length)}%` }}
                onPointerDown={(e) => {
                  e.preventDefault();
                  capture(e);
                  setDrag(cut.id);
                  dragTo.current = null;
                }}
                onPointerMove={(e) => {
                  if (drag !== cut.id) return;
                  dragTo.current = posAt(e.clientX, e.clientY);
                  onMove(cut.id, dragTo.current);
                }}
                onPointerUp={() => {
                  if (dragTo.current !== null) onCommit(cut.id, dragTo.current);
                  dragTo.current = null;
                  setDrag(null);
                }}
                onPointerCancel={() => {
                  if (dragTo.current !== null) onCommit(cut.id, dragTo.current);
                  dragTo.current = null;
                  setDrag(null);
                }}
              >
                <div
                  className={`absolute ${
                    across
                      ? 'inset-y-0 left-1/2 w-[2px] -translate-x-1/2'
                      : 'inset-x-0 top-1/2 h-[2px] -translate-y-1/2'
                  } ${drag === cut.id ? 'bg-accent' : 'bg-accent/75'}`}
                />
                <div
                  className={`absolute flex items-center gap-1 ${
                    across ? 'left-1/2 top-1 -translate-x-1/2' : 'left-1 top-1/2 -translate-y-1/2'
                  }`}
                >
                  {edit?.id === cut.id ? (
                    <input
                      type="text"
                      inputMode="numeric"
                      autoFocus
                      value={edit.text}
                      onChange={(e) => setEdit({ id: cut.id, text: e.target.value })}
                      onBlur={commitEdit}
                      onKeyDown={(e) => {
                        if (e.key === 'Enter') commitEdit();
                        else if (e.key === 'Escape') setEdit(null);
                      }}
                      onPointerDown={(e) => e.stopPropagation()}
                      onFocus={(e) => e.target.select()}
                      className="w-[64px] rounded bg-ink-900/95 px-1.5 py-0.5 text-[10px] tabular-nums text-accent outline outline-1 outline-accent"
                      aria-label={`第 ${i + 1} 条切割线的位置（像素）`}
                    />
                  ) : (
                    <button
                      type="button"
                      title="点击输入精确位置"
                      onPointerDown={(e) => e.stopPropagation()}
                      onClick={(e) => {
                        e.stopPropagation();
                        setEdit({ id: cut.id, text: String(cut.pos) });
                      }}
                      className="rounded bg-ink-900/90 px-1.5 py-0.5 text-[10px] font-medium tabular-nums text-accent hover:bg-ink-900"
                    >
                      {i + 1} · {cut.pos}px
                    </button>
                  )}
                  <button
                    type="button"
                    aria-label="删除这条切割线"
                    onPointerDown={(e) => e.stopPropagation()}
                    onClick={(e) => {
                      e.stopPropagation();
                      onRemove(cut.id);
                    }}
                    className="grid h-6 w-6 place-content-center rounded bg-ink-900/90 text-xs text-rose-300 active:bg-rose-500/30"
                  >
                    ✕
                  </button>
                </div>
              </div>
            ))}
          </div>
        </div>
      </div>

      <button
        type="button"
        onClick={addAtCentre}
        className="absolute right-3 bottom-3 rounded-full border border-ink-600 bg-ink-900/95 px-4 py-2 text-sm font-medium text-accent shadow-lg backdrop-blur active:scale-95"
      >
        ＋ 在画面中央添加
      </button>
    </div>
  );
}
