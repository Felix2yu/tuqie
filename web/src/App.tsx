import { useCallback, useMemo, useState } from 'react';
import { analyze as analyzeFile, sliceUrl } from './api';
import Controls from './components/Controls';
import Dropzone from './components/Dropzone';
import Lightbox from './components/Lightbox';
import PiecesGrid from './components/PiecesGrid';
import Rail from './components/Rail';
import Stage from './components/Stage';
import {
  MIN_PIECE_PX,
  axisLength,
  bandLabel,
  bandsFromCuts,
  clampCut,
  extFor,
  normalize,
  pieceName,
  resolveCuts,
  type Band,
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
  const [removed, setRemoved] = useState<number[]>([]);
  const [manual, setManual] = useState<number[]>([]);
  const [fit, setFit] = useState<'fill' | 'page'>('fill');
  const [settings, setSettings] = useState<Settings>({ format: 'jpeg', quality: 90 });
  const [viewport, setViewport] = useState<[number, number]>([0, 1]);

  const [busy, setBusy] = useState<string | null>(null);
  const [status, setStatus] = useState<Status | null>(null);
  const [zoom, setZoom] = useState<Band | null>(null);

  const shareAvailable = useMemo(() => canShareFiles(), []);
  const axis = analysis?.axis ?? 'y';
  // Every cut position, band and drag is measured along the stitch direction.
  const length = analysis ? axisLength(axis, analysis.width, analysis.height) : 0;

  const cuts = useMemo(
    () => (analysis ? resolveCuts(analysis.candidates, threshold, removed, manual, length) : []),
    [analysis, threshold, removed, manual, length],
  );
  const bands = useMemo(() => bandsFromCuts(cuts, length), [cuts, length]);
  const keptCount = useMemo(
    () => (analysis ? analysis.candidates.filter((c) => c.score >= threshold).length : 0),
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
    setStatus(null);
    setUploadError(null);
    setZoom(null);
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
      setStatus(null);
    } catch (err) {
      setUploadError((err as Error).message);
    } finally {
      setUploading(false);
    }
  };

  const addCut = (pos: number) => {
    if (!analysis) return;
    if (cuts.some((c) => Math.abs(c - pos) < MIN_PIECE_PX)) return;
    setManual((m) => normalize([...m, pos], length));
    setStatus(null);
  };

  const moveCut = (index: number, pos: number) => {
    const old = cuts[index];
    if (old === undefined) return;
    const next = clampCut(pos, cuts, index, length);
    if (next === old) return;
    // The line the user dragged away from must not reappear on the next render.
    setRemoved((r) => (r.includes(old) ? r : [...r, old]));
    setManual((m) => normalize([...m.filter((v) => v !== old), next], length));
  };

  const removeCut = (pos: number) => {
    setRemoved((r) => (r.includes(pos) ? r : [...r, pos]));
    setManual((m) => m.filter((v) => v !== pos));
  };

  const exportPieces = async (): Promise<void> => {
    if (!analysis) return;
    const total = bands.length;
    if (shareAvailable) {
      const pieces = await renderPieces(
        analysis.id,
        analysis.axis,
        bands,
        analysis.filename,
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
      bands,
      analysis.filename,
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
      const blob = await zipPieces(analysis.id, analysis.axis, cuts, settings.format, settings.quality);
      downloadBlob(blob, `${analysis.filename.replace(/\.[^./]*$/, '')}-slices.zip`);
      setStatus({ tone: 'ok', text: `ZIP 已下载，共 ${bands.length} 张` });
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
          {analysis.filename} · {analysis.width}×{analysis.height} · {bands.length} 张
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
          />
        </div>

        <aside className="thin-scroll safe-bottom flex max-h-[48vh] shrink-0 flex-col gap-4 overflow-y-auto border-t border-ink-700 bg-ink-900 p-3 md:max-h-none md:w-[360px] md:border-l md:border-t-0">
          <Controls
            analysis={analysis}
            axis={analysis.axis}
            pieceCount={bands.length}
            threshold={threshold}
            keptCount={keptCount}
            candidateCount={analysis.candidates.length}
            fit={fit}
            settings={settings}
            busy={busy}
            status={status}
            shareAvailable={shareAvailable}
            onThreshold={setThreshold}
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
              onPick={setZoom}
            />
          </div>
          {zoom && (
            <Lightbox
              src={sliceUrl(
                analysis.id,
                analysis.axis,
                zoom.from,
                zoom.to,
                zoom.index,
                settings.format,
                settings.quality,
              )}
              name={pieceName(analysis.filename, zoom.index, extFor(settings.format))}
              label={`${zoom.index + 1} · ${bandLabel(analysis.axis, analysis.width, analysis.height, zoom)}`}
              onClose={() => setZoom(null)}
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
