import { useEffect, useRef, useState } from 'react';
import { positionOf } from '../lib/cuts';
import type { Axis, Candidate, Signal } from '../types';

type Props = {
  axis: Axis;
  /** How far cut positions run: height for a vertical stitch, width for a horizontal one. */
  length: number;
  signal: Signal;
  cuts: number[];
  candidates: Candidate[];
  viewport: [number, number];
  onAdd: (pos: number) => void;
  onMove: (index: number, pos: number) => void;
  onCommit: (index: number, pos: number) => void;
};

const HIT_PX = 12;

/** Navigator strip: line-to-line activity for the whole screenshot at once. */
export default function Rail({ axis, length, signal, cuts, candidates, viewport, onAdd, onMove, onCommit }: Props) {
  const box = useRef<HTMLDivElement>(null);
  const canvas = useRef<HTMLCanvasElement>(null);
  const [drag, setDrag] = useState<number | null>(null);
  const dragTo = useRef<number | null>(null);

  // The profile runs along the strip's long side: down for rows, across for columns.
  const along = axis === 'x';

  useEffect(() => {
    const el = box.current;
    const cv = canvas.current;
    if (!el || !cv) return;

    const draw = () => {
      const dpr = window.devicePixelRatio || 1;
      const w = el.clientWidth;
      const h = el.clientHeight;
      if (w === 0 || h === 0) return;
      cv.width = Math.round(w * dpr);
      cv.height = Math.round(h * dpr);
      cv.style.width = `${w}px`;
      cv.style.height = `${h}px`;
      const ctx = cv.getContext('2d');
      if (!ctx) return;
      ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
      ctx.clearRect(0, 0, w, h);

      const lines = signal.lines || 1;
      const span = along ? w : h;
      const across = along ? h : w;
      const cell = span / lines;
      const inView = (i: number) => {
        const t = i / lines;
        return t >= viewport[0] - 0.01 && t <= viewport[1] + 0.01;
      };
      for (let i = 0; i < lines; i++) {
        const p = i * cell;
        const flat = signal.flat[i] ?? 0;
        ctx.fillStyle = `rgba(226,232,240,${0.035 + flat * 0.075})`;
        if (along) ctx.fillRect(p, 0, cell + 1, h);
        else ctx.fillRect(0, p, w, cell + 1);
        const diff = signal.diff[i] ?? 0;
        const len = Math.max(1, diff * (across - 8));
        ctx.fillStyle = inView(i) ? 'rgba(45,212,191,0.9)' : 'rgba(45,212,191,0.45)';
        if (along) ctx.fillRect(p, 4, Math.max(1, cell), len);
        else ctx.fillRect(4, p, len, Math.max(1, cell));
      }

      // Lines the threshold currently hides, so the user can see what they gave up.
      ctx.fillStyle = 'rgba(148,163,184,0.55)';
      const taken = new Set(cuts);
      for (const c of candidates) {
        if (taken.has(c.pos)) continue;
        const p = (c.pos / length) * span;
        if (along) ctx.fillRect(p - 0.5, 0, 1, h);
        else ctx.fillRect(0, p - 0.5, w, 1);
      }

      cuts.forEach((pos, i) => {
        const p = positionOf(pos, length) / 100 * span;
        ctx.fillStyle = '#5eead4';
        if (along) {
          ctx.fillRect(p - 1, 0, 2, h);
          ctx.beginPath();
          ctx.moveTo(p - 4, h);
          ctx.lineTo(p + 4, h);
          ctx.lineTo(p, h - 5);
          ctx.closePath();
          ctx.fill();
        } else {
          ctx.fillRect(0, p - 1, w, 2);
          ctx.beginPath();
          ctx.moveTo(w, p - 4);
          ctx.lineTo(w, p + 4);
          ctx.lineTo(w - 5, p);
          ctx.closePath();
          ctx.fill();
        }
        ctx.font = '9px ui-sans-serif, system-ui';
        ctx.fillStyle = '#0f172a';
        ctx.fillText(String(i + 1), along ? p + 2 : 2, along ? 10 : p + 3);
      });
    };

    draw();
    const ro = new ResizeObserver(draw);
    ro.observe(el);
    return () => ro.disconnect();
  }, [along, length, signal, cuts, candidates, viewport]);

  const posAt = (clientX: number, clientY: number) => {
    const el = box.current;
    if (!el) return 0;
    const rect = el.getBoundingClientRect();
    const span = along ? rect.width : rect.height;
    if (span <= 0) return 0;
    const ratio = Math.min(Math.max((along ? clientX - rect.left : clientY - rect.top) / span, 0), 1);
    return Math.round(ratio * length);
  };

  const nearestCut = (pos: number) => {
    const boxPx = (along ? box.current?.clientWidth : box.current?.clientHeight) || 1;
    const tolerance = (HIT_PX / boxPx) * length;
    let best = -1;
    let bestGap = Infinity;
    cuts.forEach((c, i) => {
      const gap = Math.abs(c - pos);
      if (gap < tolerance && gap < bestGap) {
        bestGap = gap;
        best = i;
      }
    });
    return best;
  };

  const endDrag = () => {
    if (drag !== null && dragTo.current !== null) onCommit(drag, dragTo.current);
    dragTo.current = null;
    setDrag(null);
  };

  // Capture can fail on a pointer the browser already released, and then a finger
  // lifted outside the rail would leave the previewed line uncommitted.
  useEffect(() => {
    if (drag === null) return;
    window.addEventListener('pointerup', endDrag);
    window.addEventListener('pointercancel', endDrag);
    return () => {
      window.removeEventListener('pointerup', endDrag);
      window.removeEventListener('pointercancel', endDrag);
    };
  });

  return (
    <div
      ref={box}
      className={
        along
          ? 'grab relative h-14 w-full shrink-0 border-t border-ink-700 bg-ink-900'
          : 'grab relative w-14 shrink-0 border-l border-ink-700 bg-ink-900 md:w-16'
      }
      onPointerDown={(e) => {
        e.preventDefault();
        try {
          e.currentTarget.setPointerCapture(e.pointerId);
        } catch {
          /* a pointer released mid-tap makes capture throw; the tap still counts */
        }
        const pos = posAt(e.clientX, e.clientY);
        const i = nearestCut(pos);
        if (i >= 0) {
          setDrag(i);
          dragTo.current = null;
        } else {
          onAdd(pos);
        }
      }}
      onPointerMove={(e) => {
        if (drag === null) return;
        dragTo.current = posAt(e.clientX, e.clientY);
        onMove(drag, dragTo.current);
      }}
      onPointerUp={endDrag}
      onPointerCancel={endDrag}
    >
      <canvas ref={canvas} className="block h-full w-full" />
      <span
        className={
          along
            ? 'pointer-events-none absolute inset-y-0 right-1 flex items-center text-[9px] tracking-wider text-ink-400'
            : 'pointer-events-none absolute inset-x-0 bottom-0 text-center text-[9px] tracking-wider text-ink-400'
        }
      >
        强度
      </span>
    </div>
  );
}
