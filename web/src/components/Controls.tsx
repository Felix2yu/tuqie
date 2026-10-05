import type { Analysis, Axis, Format, Settings } from '../types';

type Props = {
  analysis: Analysis;
  axis: Axis;
  pieceCount: number;
  threshold: number;
  keptCount: number;
  candidateCount: number;
  fit: 'fill' | 'page';
  settings: Settings;
  busy: string | null;
  status: { tone: 'ok' | 'warn' | 'err'; text: string } | null;
  shareAvailable: boolean;
  onThreshold: (v: number) => void;
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
  const jpeg = p.settings.format === 'jpeg';

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

      <div className="flex items-center gap-3">
        <span className="w-12 shrink-0 text-xs text-ink-400">格式</span>
        <div className="flex overflow-hidden rounded-lg border border-ink-600">
          {(['jpeg', 'png'] as Format[]).map((f) => (
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
        {jpeg ? (
          <>
            <input
              type="range"
              min={60}
              max={100}
              value={p.settings.quality}
              onChange={(e) => p.onSettings({ ...p.settings, quality: Number(e.target.value) })}
              className="min-w-0 flex-1"
              aria-label="JPEG 质量"
            />
            <span className="w-8 shrink-0 text-right text-xs tabular-nums text-ink-400">{p.settings.quality}</span>
          </>
        ) : (
          <span className="flex-1 text-right text-xs text-ink-400">PNG 无损</span>
        )}
      </div>

      <div className="flex items-center gap-2">
        <button
          type="button"
          disabled={p.busy !== null || p.pieceCount < 1}
          onClick={p.onSaveToPhotos}
          className="flex-1 rounded-lg bg-accent px-3 py-2.5 text-sm font-semibold text-ink-900 disabled:opacity-40 active:scale-[0.98]"
        >
          {p.shareAvailable ? `存入相册 · ${p.pieceCount} 张` : `导出 ${p.pieceCount} 张`}
        </button>
        <button
          type="button"
          disabled={p.busy !== null}
          onClick={p.onDownloadZip}
          className="rounded-lg border border-ink-600 bg-ink-800 px-3 py-2.5 text-sm text-slate-200 disabled:opacity-40 active:scale-[0.98]"
        >
          ZIP
        </button>
      </div>

      {p.busy ? (
        <p className="text-xs text-accent">{p.busy}</p>
      ) : p.status ? (
        <p className={`text-xs ${TONE[p.status.tone]}`}>{p.status.text}</p>
      ) : (
        <p className="text-xs text-ink-400">
          {p.analysis.width}×{p.analysis.height} · {p.axis === 'x' ? '竖向切线' : '横向切线'} ·
          拖动青线调整，点 ✕ 删除，强度条上点一下加一条
        </p>
      )}
    </div>
  );
}
