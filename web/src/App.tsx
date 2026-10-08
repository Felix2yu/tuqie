import { useCallback, useMemo, useRef, useState } from 'react';
import { analyze as analyzeFile, sliceUrl } from './api';
import Controls from './components/Controls';
import Dropzone from './components/Dropzone';
import Lightbox from './components/Lightbox';
import PiecesGrid from './components/PiecesGrid';
import Rail from './components/Rail';
import Stage from './components/Stage';
import {
  MIN_PIECE_PX,
  applyMove,
  axisLength,
  bandLabel,
  bandsFromCuts,
  clampCut,
  extFor,
  namePad,
  normalizeCuts,
  pieceName,
  resolveCuts,
  sanitizePrefix,
  splitCuts,
  stemOf,
  type Band,
  type Cut,
  type Naming,
  type SplitMode,
} from './lib/cuts';
import {
  canShareFiles,
  downloadBlob,
  isIOS,
  renderPieces,
  sharePieces,
  zipPieces,
} from './lib/export';
import type { Analysis, Settings } from './types';

type Status = { tone: 'ok' | 'warn' | 'err'; text: string };

// Cuts snapped onto real padding score >= .55; a bare content jump needs the
// picture itself to change before it clears this line. Everything weaker stays
// behind the sensitivity slider.
const DEFAULT_THRESHOLD = 0.45;

export default function App() {
  const [analysis, setAnalysis] = useState<Analysis | null>(null);
  const [uploading, setUploading] = useState(false);
  const [uploadError, setUploadError] = useState<string | null>(null);

  const [threshold, setThreshold] = useState(DEFAULT_THRESHOLD);
  const [removed, setRemoved] = useState<string[]>([]);
  const [manual, setManual] = useState<Cut[]>([]);
  // Where a line being dragged currently is. Only the released position becomes a
  // cut: banning every pixel the pointer passed over is how lines went missing.
  const [drag, setDrag] = useState<{ id: string; pos: number } | null>(null);
  const [splitMode, setSplitMode] = useState<SplitMode>('count');
  const [splitValue, setSplitValue] = useState(4);
  const [skipped, setSkipped] = useState<string[]>([]);
  const [naming, setNaming] = useState<Naming>({ prefix: '', start: 1 });
  const [fit, setFit] = useState<'fill' | 'page'>('fill');
  const [settings, setSettings] = useState<Settings>({ format: 'jpeg', quality: 90 });
  const [viewport, setViewport] = useState<[number, number]>([0, 1]);

  const [busy, setBusy] = useState<string | null>(null);
  const [status, setStatus] = useState<Status | null>(null);
  // The id of the piece the lightbox is showing, not a snapshot of it: the crop,
  // the number and the name are read from the live list every render.
  const [zoomId, setZoomId] = useState<string | null>(null);
  const lineSeq = useRef(0);

  const shareAvailable = useMemo(() => canShareFiles(), []);
  const axis = analysis?.axis ?? 'y';
  // Every cut position, band and drag is measured along the stitch direction.
  const length = analysis ? axisLength(axis, analysis.width, analysis.height) : 0;

  const baseCuts = useMemo(
    () => (analysis ? resolveCuts(analysis.candidates, threshold, removed, manual, length) : []),
    [analysis, threshold, removed, manual, length],
  );
  const cuts = useMemo(() => {
    if (!drag) return baseCuts;
    const i = baseCuts.findIndex((c) => c.id === drag.id);
    if (i < 0) return baseCuts;
    const next = [...baseCuts];
    next[i] = { ...next[i], pos: clampCut(drag.pos, baseCuts.map((c) => c.pos), i, length) };
    return next;
  }, [drag, baseCuts, length]);
  const bands = useMemo(() => bandsFromCuts(cuts, length), [cuts, length]);
  const skipSet = useMemo(() => new Set(skipped), [skipped]);
  // Export numbering runs over the kept pieces, so dropping one closes the gap.
  const exported = useMemo(() => bands.filter((b) => !skipSet.has(b.id)), [bands, skipSet]);
  const ordinals = useMemo(() => {
    const m = new Map<string, number>();
    for (const b of exported) m.set(b.id, m.size);
    return m;
  }, [exported]);
  const ordinalOf = useCallback((band: Band) => ordinals.get(band.id) ?? 0, [ordinals]);
  const zoomBand = useMemo(
    () => (zoomId === null ? null : (bands.find((b) => b.id === zoomId) ?? null)),
    [zoomId, bands],
  );
  const keptCount = useMemo(
    () =>
      analysis && threshold < 1
        ? analysis.candidates.filter((c) => c.score >= threshold).length
        : 0,
    [analysis, threshold],
  );

  const onViewport = useCallback((window: [number, number]) => {
    const q = (v: number) => Math.round(v * 100) / 100;
    setViewport(([a, b]) => (q(a) === q(window[0]) && q(b) === q(window[1]) ? [a, b] : window));
  }, []);

  const reset = () => {
    setAnalysis(null);
    setThreshold(DEFAULT_THRESHOLD);
    setRemoved([]);
    setManual([]);
    setSkipped([]);
    setDrag(null);
    setStatus(null);
    setUploadError(null);
    setZoomId(null);
  };

  const handleFile = async (file: File) => {
    setUploading(true);
    setUploadError(null);
    try {
      const res = await analyzeFile(file);
      setAnalysis(res);
      // A wide strip of photos is read side to side, so show all of its height
      // rather than stretching it across the viewport.
      setFit(res.axis === 'x' ? 'page' : 'fill');
      setRemoved([]);
      setManual([]);
      setSkipped([]);
      setDrag(null);
      // The uploaded file's own name is the sensible starting prefix.
      setNaming({ prefix: stemOf(res.filename), start: 1 });
      setStatus(null);
    } catch (err) {
      setUploadError((err as Error).message);
    } finally {
      setUploading(false);
    }
  };

  const addCut = (pos: number) => {
    if (!analysis) return;
    if (cuts.some((c) => Math.abs(c.pos - pos) < MIN_PIECE_PX)) return;
    setManual((m) => normalizeCuts([...m, { id: `m${++lineSeq.current}`, pos }], length));
    setStatus(null);
  };

  const moveCut = (id: string, pos: number) => setDrag({ id, pos });

  const commitCut = (id: string, pos: number) => {
    setDrag(null);
    const moved = applyMove(baseCuts, id, pos, length, manual, removed, `m${++lineSeq.current}`);
    if (!moved) return;
    setManual(moved.manual);
    setRemoved(moved.removed);
  };

  const removeCut = (id: string) => {
    if (id.startsWith('m')) {
      setManual((m) => m.filter((c) => c.id !== id));
      return;
    }
    setRemoved((r) => (r.includes(id) ? r : [...r, id]));
  };

  // A regular grid is what the user asked for, so it replaces the detected lines
  // rather than mixing with them: the threshold goes to its top position, which
  // now means "no detected line", and the grid lines become the only cuts.
  const applySplit = () => {
    if (!analysis) return;
    const next = splitCuts(splitMode, splitValue, length);
    if (next.length === 0) {
      setStatus({ tone: 'warn', text: '这个数值在这张图上分不出切片' });
      return;
    }
    setThreshold(1);
    setRemoved([]);
    setManual(next.map((pos) => ({ id: `m${++lineSeq.current}`, pos })));
    setDrag(null);
    setStatus({
      tone: 'ok',
      text:
        splitMode === 'count'
          ? `已按 ${splitValue} 等分重排 · ${next.length + 1} 张`
          : `已按每段 ${splitValue}px 重排 · ${next.length + 1} 张`,
    });
  };

  const toggleSkip = (id: string) =>
    setSkipped((s) => (s.includes(id) ? s.filter((v) => v !== id) : [...s, id]));

  const exportPieces = async (): Promise<void> => {
    if (!analysis) return;
    const total = exported.length;
    if (shareAvailable) {
      const pieces = await renderPieces(
        analysis.id,
        analysis.axis,
        exported,
        naming,
        settings.format,
        settings.quality,
        (done, t) => setBusy(`正在生成第 ${done}/${t} 张…`),
      );
      setBusy('请在分享面板中选择「存储图像」…');
      const outcome = await sharePieces(pieces, analysis.taken);
      if (outcome === 'shared') setStatus({ tone: 'ok', text: `已交给系统，${total} 张已存入相册` });
      else if (outcome === 'cancelled') setStatus({ tone: 'warn', text: '已取消分享' });
      else setStatus({ tone: 'err', text: '分享失败，请改用 ZIP 下载' });
      return;
    }
    const pieces = await renderPieces(
      analysis.id,
      analysis.axis,
      exported,
      naming,
      settings.format,
      settings.quality,
      (done, t) => setBusy(`正在生成第 ${done}/${t} 张…`),
    );
    pieces.forEach((piece) => downloadBlob(piece.blob, piece.name));
    setStatus({ tone: 'ok', text: `已下载 ${total} 张到本地` });
  };

  const onSaveToPhotos = async () => {
    setBusy('正在准备…');
    setStatus(null);
    try {
      await exportPieces();
    } catch (err) {
      setStatus({ tone: 'err', text: (err as Error).message });
    } finally {
      setBusy(null);
    }
  };

  const onDownloadZip = async () => {
    if (!analysis) return;
    setBusy('正在打包 ZIP…');
    setStatus(null);
    try {
      const blob = await zipPieces(
        analysis.id,
        analysis.axis,
        cuts.map((c) => c.pos),
        bands.reduce<number[]>((acc, b, i) => (skipSet.has(b.id) ? [...acc, i] : acc), []),
        naming,
        settings.format,
        settings.quality,
      );
      const prefix = sanitizePrefix(naming.prefix) || stemOf(analysis.filename);
      downloadBlob(blob, `${prefix}-slices.zip`);
      const left = bands.length - exported.length;
      setStatus({
        tone: 'ok',
        text: `ZIP 已下载，共 ${exported.length} 张${left ? `（跳过 ${left} 张）` : ''}`,
      });
    } catch (err) {
      setStatus({ tone: 'err', text: (err as Error).message });
    } finally {
      setBusy(null);
    }
  };

  if (!analysis) {
    return (
      <Dropzone busy={uploading} error={uploadError} onFile={(f) => void handleFile(f)} />
    );
  }

  return (
    <div className="flex h-full flex-col">
      <header className="flex items-center gap-3 border-b border-ink-700 bg-ink-900 px-3 py-2">
        <span className="text-sm font-semibold text-white">图切</span>
        <span className="min-w-0 flex-1 truncate text-xs text-ink-400">
          {analysis.filename} · {analysis.width}×{analysis.height} · {exported.length} 张
          {exported.length < bands.length ? `（跳过 ${bands.length - exported.length}）` : ''}
        </span>
        <button
          type="button"
          onClick={reset}
          className="rounded-lg border border-ink-600 bg-ink-800 px-3 py-1.5 text-xs text-slate-200 active:scale-95"
        >
          换一张
        </button>
      </header>

      <main className="flex min-h-0 flex-1 flex-col md:flex-row">
        {/* A horizontal stitch is navigated by a strip under the picture instead
            of beside it, so the rail follows the cut direction. */}
        <div className={`flex min-h-0 flex-1 ${analysis.axis === 'x' ? 'flex-col' : ''}`}>
          <Stage
            src={analysis.url}
            axis={analysis.axis}
            imageWidth={analysis.width}
            imageHeight={analysis.height}
            cuts={cuts}
            fit={fit}
            onAdd={addCut}
            onMove={moveCut}
            onCommit={commitCut}
            onRemove={removeCut}
            onViewport={onViewport}
          />
          <Rail
            axis={analysis.axis}
            length={length}
            signal={analysis.signal}
            cuts={cuts}
            candidates={analysis.candidates}
            viewport={viewport}
            onAdd={addCut}
            onMove={moveCut}
            onCommit={commitCut}
          />
        </div>

        <aside className="thin-scroll safe-bottom flex max-h-[48vh] shrink-0 flex-col gap-4 overflow-y-auto border-t border-ink-700 bg-ink-900 p-3 md:max-h-none md:w-[360px] md:border-l md:border-t-0">
          <Controls
            analysis={analysis}
            axis={analysis.axis}
            length={length}
            pieceCount={exported.length}
            threshold={threshold}
            keptCount={keptCount}
            candidateCount={analysis.candidates.length}
            splitMode={splitMode}
            splitValue={splitValue}
            naming={naming}
            fit={fit}
            settings={settings}
            busy={busy}
            status={status}
            shareAvailable={shareAvailable}
            onThreshold={setThreshold}
            onSplitMode={setSplitMode}
            onSplitValue={setSplitValue}
            onApplySplit={applySplit}
            onNaming={setNaming}
            onFit={setFit}
            onAcceptAll={() => {
              setRemoved([]);
              setManual([]);
              setThreshold(0);
            }}
            onClearAll={() => {
              setRemoved([]);
              setManual([]);
              setThreshold(1);
            }}
            onSettings={setSettings}
            onSaveToPhotos={() => void onSaveToPhotos()}
            onDownloadZip={() => void onDownloadZip()}
          />
          <div>
            <div className="mb-2 text-xs text-ink-400">切片预览</div>
            <PiecesGrid
              src={analysis.url}
              axis={analysis.axis}
              imageWidth={analysis.width}
              imageHeight={analysis.height}
              bands={bands}
              skipped={skipSet}
              onToggleSkip={toggleSkip}
              onPick={(b) => setZoomId(b.id)}
            />
          </div>
          {zoomBand && (
            <Lightbox
              src={sliceUrl(
                analysis.id,
                analysis.axis,
                zoomBand.from,
                zoomBand.to,
                zoomBand.index,
                settings.format,
                settings.quality,
              )}
              name={pieceName(
                naming,
                ordinalOf(zoomBand),
                extFor(settings.format),
                namePad(naming.start, exported.length),
              )}
              label={`${ordinalOf(zoomBand) + 1} · ${bandLabel(analysis.axis, analysis.width, analysis.height, zoomBand)}`}
              onClose={() => setZoomId(null)}
            />
          )}
          {isIOS() && !shareAvailable && (
            <p className="text-xs text-amber-300">
              此浏览器不支持把图片直接写入相册，请用 Safari 打开后重试。
            </p>
          )}
        </aside>
      </main>
    </div>
  );
}
