import { useRef, useState } from 'react';

type Props = {
  busy: boolean;
  error: string | null;
  onFile: (file: File) => void;
};

export default function Dropzone({ busy, error, onFile }: Props) {
  const input = useRef<HTMLInputElement>(null);
  const [over, setOver] = useState(false);

  const pick = (files: FileList | null) => {
    const file = files?.[0];
    if (file) onFile(file);
  };

  return (
    <div className="flex min-h-full flex-col items-center justify-center gap-8 px-6 py-12">
      <header className="text-center">
        <h1 className="text-3xl font-semibold tracking-tight text-white">图切</h1>
        <p className="mt-2 text-sm text-ink-400">把拼成长图的截图拆回一张张独立照片</p>
      </header>

      <button
        type="button"
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
        className={`w-full max-w-md rounded-2xl border border-dashed px-6 py-14 text-center transition ${
          over ? 'border-accent bg-accent-dim/20' : 'border-ink-600 bg-ink-900 hover:border-ink-400'
        }`}
      >
        <div className="text-5xl leading-none text-accent">✂</div>
        <div className="mt-4 text-base font-medium text-white">
          {busy ? '正在识别分割线…' : '拖入或点击选择长截图'}
        </div>
        <div className="mt-1 text-xs text-ink-400">
          支持 PNG / JPEG / GIF，最大 250 MB
        </div>
      </button>

      <ul className="max-w-md space-y-1 text-center text-xs text-ink-400">
        <li>竖排、横排的长图都会自动判方向，按拼接处的画面突变定位切割线，也可手动增删与拖动。</li>
        <li>iPhone / iPad 用 Safari 打开，导出时选择「存储图像」即可批量存入相册。</li>
        <li>从相册选图上传时，iOS 会自动把 HEIC 转成 JPEG。</li>
      </ul>

      {busy && (
        <div className="h-1 w-40 overflow-hidden rounded-full bg-ink-800">
          <div className="h-full w-1/3 animate-[slide_1.1s_infinite] rounded-full bg-accent" />
        </div>
      )}
      {error && <p className="max-w-md text-center text-sm text-rose-400">{error}</p>}

      <input
        ref={input}
        type="file"
        accept="image/png,image/jpeg,image/gif"
        aria-label="选择长截图文件"
        className="sr-only"
        onChange={(e) => pick(e.target.files)}
      />
      <style>{`@keyframes slide{0%{transform:translateX(-100%)}100%{transform:translateX(320%)}}`}</style>
    </div>
  );
}
