import { useEffect, useRef, useState } from 'react';
import { humanBytes } from '../lib/size';
import ThemeToggle from './ThemeToggle';

type Props = {
  busy: boolean;
  error: string | null;
  // The shell opens without a network; a picture only becomes pieces on the
  // server, so this is the one thing the page can say for itself.
  offline: boolean;
  // Bytes one upload may carry on this instance, or null while the server has not
  // answered. Zero is the ceiling switched off.
  maxUpload: number | null;
  onFile: (file: File) => void;
};

const PASTED = 'pasted';

const NOTES = [
  '竖排、横排的长图都会自动判方向，按拼接处的画面突变定位切割线，也可手动增删与拖动。',
  '手机照片自带的旋转标记会被读进来，切出来的就是你看到的那个方向。',
  'iPhone / iPad 用 Safari 打开，导出时选择「存储图像」即可批量存入相册。',
  '手机拍的 HEIC 可以直接上传，不用先转成 JPEG。',
];

/**
 * A screenshot waiting in the pasteboard arrives without a usable name, and the
 * server only accepts what it can tell apart, so give it one from its own type.
 */
function namePasted(file: File): File | null {
  const ext =
    file.type === 'image/jpeg' ? 'jpg' : file.type === 'image/gif' ? 'gif' : file.type === 'image/png' ? 'png' : '';
  if (!ext) return null;
  if (/\.(png|jpe?g|gif)$/i.test(file.name)) return file;
  const stamp = new Date().toISOString().slice(0, 19).replace(/[-:T]/g, '');
  return new File([file], `${PASTED}-${stamp}.${ext}`, { type: file.type });
}

export default function Dropzone({ busy, error, offline, maxUpload, onFile }: Props) {
  const input = useRef<HTMLInputElement>(null);
  const [over, setOver] = useState(false);
  const send = useRef(onFile);
  send.current = onFile;
  // A drop or a click that lands while an upload is in flight would start a second
  // analyze, and whichever came back last would replace the picture in view.
  const busyRef = useRef(busy);
  busyRef.current = busy;

  const pick = (files: FileList | null) => {
    if (busyRef.current) return;
    const file = files?.[0];
    if (file) send.current(file);
  };

  useEffect(() => {
    if (busy) return;
    const onPaste = (e: ClipboardEvent) => {
      const data = e.clipboardData;
      if (!data) return;
      const candidates = [...data.items]
        .filter((i) => i.kind === 'file')
        .map((i) => i.getAsFile())
        .concat([...data.files]);
      for (const raw of candidates) {
        const file = raw && namePasted(raw);
        if (!file) continue;
        e.preventDefault();
        send.current(file);
        return;
      }
    };
    window.addEventListener('paste', onPaste);
    return () => window.removeEventListener('paste', onPaste);
  }, [busy]);

  // No answer from the server leaves the size out of the sentence rather than in it
  // with a number this instance may not use.
  const sizeNote = maxUpload === null ? null : maxUpload === 0 ? '不限大小' : `最大 ${humanBytes(maxUpload)}`;

  return (
    <div className="relative flex min-h-full flex-col">
      {/* The one control on this page that is not about the picture, so it stays
          out of the way of both the heading and the drop zone. */}
      <div className="safe-top safe-right absolute right-0 top-0 z-10 p-3">
        <ThemeToggle />
      </div>

      {/* flex-1 rather than a second min-h-full: a percentage height against a
          parent that only has a min-height resolves to nothing, and the block
          would sit at the top of a tall window instead of in the middle. */}
      <div className="mx-auto flex w-full max-w-5xl flex-1 flex-col justify-center gap-8 px-5 py-14 sm:px-8 lg:gap-12">
        <header className="text-center lg:text-left">
          <h1 className="text-3xl font-semibold tracking-tight text-ink lg:text-4xl">图切</h1>
          <p className="mt-2 text-sm text-muted lg:text-base">把拼成长图的截图拆回一张张独立照片</p>
        </header>

        {/* A desktop window is mostly empty air if this stays the phone's single
            column, so the notes move beside the drop zone instead of under it. */}
        <div className="grid gap-6 lg:grid-cols-[minmax(0,1.25fr)_minmax(0,1fr)] lg:gap-8">
          <div className="flex min-w-0 flex-col gap-4">
            <button
              type="button"
              disabled={busy}
              onClick={() => input.current?.click()}
              onDragOver={(e) => {
                e.preventDefault();
                setOver(true);
              }}
              onDragLeave={() => setOver(false)}
              onDrop={(e) => {
                e.preventDefault();
                setOver(false);
                pick(e.dataTransfer.files);
              }}
              className={`flex flex-col justify-center rounded-2xl border border-dashed px-6 py-14 text-center transition lg:flex-1 lg:px-10 ${
                over ? 'border-accent bg-accent-dim/20' : 'border-edge bg-surface hover:border-edge-hi'
              }`}
            >
              <div className="text-5xl leading-none text-accent lg:text-6xl">✂</div>
              <div className="mt-4 text-base font-medium text-ink">
                {busy ? '正在识别分割线…' : '拖入或点击选择长截图'}
              </div>
              <div className="mt-1 text-xs text-muted">
                {'支持 PNG / JPEG / GIF / HEIC / AVIF / JXL'}
                {sizeNote ? `，${sizeNote}` : ''}
                {'，也可以直接 ⌘V / Ctrl+V 粘贴'}
              </div>
            </button>

            {offline && <p className="text-sm text-warn">现在离线：这页还开得起来，但识别与导出都在服务器，等网络回来再传。</p>}

            {busy && (
              <div className="h-1 w-40 overflow-hidden rounded-full bg-sunken">
                <div className="h-full w-1/3 animate-[slide_1.1s_infinite] rounded-full bg-accent" />
              </div>
            )}
            {error && <p className="text-sm text-danger">{error}</p>}
          </div>

          <ul className="grid gap-1 text-center text-xs text-muted lg:gap-3 lg:text-left">
            {NOTES.map((note) => (
              <li key={note} className="lg:rounded-xl lg:border lg:border-line lg:bg-surface lg:px-4 lg:py-3">
                {note}
              </li>
            ))}
          </ul>
        </div>
      </div>

      <input
        ref={input}
        type="file"
        accept="image/png,image/jpeg,image/gif,image/heic,image/heif,image/avif,image/jxl,.heic,.heif,.avif,.jxl"
        aria-label="选择长截图文件"
        className="sr-only"
        onChange={(e) => pick(e.target.files)}
      />
      <style>{`@keyframes slide{0%{transform:translateX(-100%)}100%{transform:translateX(320%)}}`}</style>
    </div>
  );
}
