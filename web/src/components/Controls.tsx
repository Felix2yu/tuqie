import {
  MIN_PIECE_PX,
  extFor,
  namePad,
  pieceName,
  splitCuts,
  splitFloor,
  stemOf,
  type Naming,
  type SplitMode,
} from '../lib/cuts';
import type { Analysis, Axis, Format, Settings } from '../types';

type Props = {
  analysis: Analysis;
  axis: Axis;
  length: number;
  pieceCount: number;
  threshold: number;
  keptCount: number;
  candidateCount: number;
  splitMode: SplitMode;
  splitValue: number;
  naming: Naming;
  fit: 'fill' | 'page';
  settings: Settings;
  busy: string | null;
  status: { tone: 'ok' | 'warn' | 'err'; text: string } | null;
  shareAvailable: boolean;
  onThreshold: (v: number) => void;
  onSplitMode: (m: SplitMode) => void;
  onSplitValue: (v: number) => void;
  onApplySplit: () => void;
  onNaming: (n: Naming) => void;
  onFit: (f: 'fill' | 'page') => void;
  onAcceptAll: () => void;
  onClearAll: () => void;
  onSettings: (s: Settings) => void;
  onSaveToPhotos: () => void;
  onDownloadZip: () => void;
};

const TONE = {
  ok: 'text-accent',
  warn: 'text-amber-300',
  err: 'text-rose-400',
} as const;

export default function Controls(p: Props) {
  // PNG is the one output without a lossy knob; the other four share the slider.
  const lossy = p.settings.format !== 'png';
  // What a container costs the receiver is not obvious from its name, so the two
  // that give something up say so where they are picked.
  const formatNote =
    p.settings.format === 'jxl'
      ? 'JXL：Chrome 里不预览，另存后交给支持它的应用'
      : p.settings.format === 'heic'
        ? 'HEIC：浏览器不预览，能直接进相册'
        : '';
  // The share sheet is the only way onto a phone's photo roll. Everywhere else a
  // single zip beats N downloads, which no browser starts without asking first.
  const [primary, secondary] = p.shareAvailable
    ? [
        { text: `存入相册 · ${p.pieceCount} 张`, title: '导出时在分享面板里选「存储图像」', run: p.onSaveToPhotos },
        { text: 'ZIP', title: '打包成一个 zip 下载', run: p.onDownloadZip },
      ]
    : [
        { text: `下载 ZIP · ${p.pieceCount} 张`, title: '打包成一个 zip 下载', run: p.onDownloadZip },
        { text: '逐张', title: '一张一个文件地下载，浏览器可能会先问你是否允许', run: p.onSaveToPhotos },
      ];

  // A chat screenshot with no seams still wants splitting, so the regular grid is
  // offered in px terms and shows what it will actually produce.
  const floor = splitFloor(p.splitMode);
  const grid = splitCuts(p.splitMode, p.splitValue, p.length);
  const canSplit = p.splitValue >= floor && grid.length > 0;
  const ext = extFor(p.settings.format);
  const pad = namePad(p.naming.start, p.pieceCount);
  const splitMax =
    p.splitMode === 'count'
      ? Math.max(floor, Math.floor(p.length / MIN_PIECE_PX))
      : Math.max(floor, p.length);

  return (
    <div className="flex flex-col gap-3">
      <div className="flex items-center gap-3">
        <span className="w-12 shrink-0 text-xs text-ink-400">灵敏度</span>
        <input
          type="range"
          min={0}
          max={100}
          value={Math.round(p.threshold * 100)}
          onChange={(e) => p.onThreshold(Number(e.target.value) / 100)}
          className="min-w-0 flex-1"
          aria-label="识别阈值"
        />
        <span className="w-16 shrink-0 text-right text-xs tabular-nums text-ink-400">
          {p.keptCount}/{p.candidateCount} 条
        </span>
      </div>

      <div className="flex items-center gap-2">
        <button
          type="button"
          onClick={p.onAcceptAll}
          className="flex-1 rounded-lg border border-ink-600 bg-ink-800 px-2 py-2 text-xs text-slate-200 active:scale-[0.98]"
        >
          接受全部识别
        </button>
        <button
          type="button"
          onClick={p.onClearAll}
          className="flex-1 rounded-lg border border-ink-600 bg-ink-800 px-2 py-2 text-xs text-slate-200 active:scale-[0.98]"
        >
          清空切割线
        </button>
        <button
          type="button"
          onClick={() => p.onFit(p.fit === 'fill' ? 'page' : 'fill')}
          className="flex-1 rounded-lg border border-ink-600 bg-ink-800 px-2 py-2 text-xs text-slate-200 active:scale-[0.98]"
        >
          {p.fit === 'fill' ? '看整图' : p.axis === 'x' ? '按高度' : '按宽度'}
        </button>
      </div>

      <div className="flex items-center gap-2">
        <span className="w-12 shrink-0 text-xs text-ink-400">切分</span>
        <div className="flex overflow-hidden rounded-lg border border-ink-600">
          {(['count', 'length'] as SplitMode[]).map((m) => (
            <button
              key={m}
              type="button"
              onClick={() => p.onSplitMode(m)}
              className={`px-2 py-1.5 text-xs ${
                p.splitMode === m ? 'bg-accent text-ink-900' : 'bg-ink-800 text-slate-300'
              }`}
            >
              {m === 'count' ? '等分' : '定长'}
            </button>
          ))}
        </div>
        <input
          type="number"
          min={floor}
          max={splitMax}
          step={1}
          value={p.splitValue}
          onChange={(e) => p.onSplitValue(Number(e.target.value))}
          onKeyDown={(e) => {
            if (e.key === 'Enter' && canSplit) p.onApplySplit();
          }}
          className="w-16 shrink-0 rounded-lg border border-ink-600 bg-ink-800 px-2 py-1.5 text-xs tabular-nums text-slate-200 outline-none focus:border-accent"
          aria-label={p.splitMode === 'count' ? '等分份数' : '每段长度（像素）'}
        />
        <span className="shrink-0 text-xs text-ink-400">{p.splitMode === 'count' ? '份' : 'px'}</span>
        <button
          type="button"
          onClick={p.onApplySplit}
          disabled={!canSplit}
          title={
            canSplit
              ? '按这个数值重排切割线，会替换当前的识别结果'
              : `至少 ${floor}${p.splitMode === 'count' ? ' 份' : 'px'}，且这张图要分得出至少两张`
          }
          className="min-w-0 flex-1 rounded-lg border border-ink-600 bg-ink-800 px-2 py-2 text-xs text-slate-200 disabled:opacity-40 active:scale-[0.98]"
        >
          {canSplit ? `应用 · ${grid.length + 1} 张` : '数值无效'}
        </button>
      </div>

      {/* The names are what ends up in the album, so the pattern is shown rather
          than left to be guessed. */}
      <div className="flex items-center gap-2">
        <span className="w-12 shrink-0 text-xs text-ink-400">命名</span>
        <input
          type="text"
          value={p.naming.prefix}
          placeholder={stemOf(p.analysis.filename)}
          onChange={(e) => p.onNaming({ ...p.naming, prefix: e.target.value })}
          className="min-w-0 flex-1 rounded-lg border border-ink-600 bg-ink-800 px-2 py-1.5 text-xs text-slate-200 outline-none focus:border-accent"
          aria-label="文件名前缀"
        />
        <span className="shrink-0 text-xs text-ink-400">从</span>
        <input
          type="number"
          min={0}
          max={9999}
          step={1}
          value={p.naming.start}
          // A fractional start would be rejected by the export endpoint, so the
          // field rounds down as it is typed.
          onChange={(e) => p.onNaming({ ...p.naming, start: Math.max(0, Math.floor(Number(e.target.value) || 0)) })}
          className="w-14 shrink-0 rounded-lg border border-ink-600 bg-ink-800 px-2 py-1.5 text-xs tabular-nums text-slate-200 outline-none focus:border-accent"
          aria-label="起始编号"
        />
        <span className="shrink-0 text-xs text-ink-400">号起</span>
      </div>
      <p className="-mt-1 truncate text-[10px] tabular-nums text-ink-400">
        {p.pieceCount > 0
          ? `导出 ${pieceName(p.naming, 0, ext, pad)}${p.pieceCount > 1 ? ` … ${pieceName(p.naming, p.pieceCount - 1, ext, pad)}` : ''}`
          : '所有切片都被排除了'}
      </p>

      <div className="flex flex-wrap items-center gap-3">
        <span className="w-12 shrink-0 text-xs text-ink-400">格式</span>
        <div className="flex shrink-0 overflow-hidden rounded-lg border border-ink-600">
          {(['jpeg', 'png', 'heic', 'avif', 'jxl'] as Format[]).map((f) => (
            <button
              key={f}
              type="button"
              onClick={() => p.onSettings({ ...p.settings, format: f })}
              className={`px-3 py-1.5 text-xs ${
                p.settings.format === f ? 'bg-accent text-ink-900' : 'bg-ink-800 text-slate-300'
              }`}
            >
              {f.toUpperCase()}
            </button>
          ))}
        </div>
        {lossy ? (
          <>
            <input
              type="range"
              min={60}
              max={100}
              value={p.settings.quality}
              onChange={(e) => p.onSettings({ ...p.settings, quality: Number(e.target.value) })}
              className="min-w-[10rem] flex-1"
              aria-label="画质"
            />
            <span className="w-8 shrink-0 text-right text-xs tabular-nums text-ink-400">{p.settings.quality}</span>
          </>
        ) : (
          <span className="flex-1 text-right text-xs text-ink-400">PNG 无损</span>
        )}
        {formatNote && <span className="w-full -mt-1 text-[10px] text-ink-400">{formatNote}</span>}
      </div>

      <div className="flex items-center gap-2">
        <button
          type="button"
          disabled={p.busy !== null || p.pieceCount < 1}
          onClick={primary.run}
          title={primary.title}
          className="flex-1 rounded-lg bg-accent px-3 py-2.5 text-sm font-semibold text-ink-900 disabled:opacity-40 active:scale-[0.98]"
        >
          {primary.text}
        </button>
        <button
          type="button"
          disabled={p.busy !== null || p.pieceCount < 1}
          onClick={secondary.run}
          title={secondary.title}
          className="rounded-lg border border-ink-600 bg-ink-800 px-3 py-2.5 text-sm text-slate-200 disabled:opacity-40 active:scale-[0.98]"
        >
          {secondary.text}
        </button>
      </div>

      {p.busy ? (
        <p className="text-xs text-accent">{p.busy}</p>
      ) : p.status ? (
        <p className={`text-xs ${TONE[p.status.tone]}`}>{p.status.text}</p>
      ) : (
        <p className="text-xs text-ink-400">
          {p.analysis.width}×{p.analysis.height} · {p.axis === 'x' ? '竖向切线' : '横向切线'} ·
          拖动青线调整，点编号输入精确位置，✕ 删除，强度条上点一下加一条
        </p>
      )}
    </div>
  );
}
