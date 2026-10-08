import { useEffect, useRef, useState } from 'react';

type Props = {
  /** The slice itself, not a crop of the upload: what this shows is what gets saved. */
  src: string;
  name: string;
  label: string;
  onClose: () => void;
};

export default function Lightbox({ src, name, label, onClose }: Props) {
  const [actual, setActual] = useState(false);
  const [natural, setNatural] = useState<[number, number] | null>(null);
  // HEIC and JXL are written out for the photo library, not for a browser: Chrome
  // refuses both. The file is still there to save, so say that instead of showing
  // a broken image.
  const [undecodable, setUndecodable] = useState(false);
  const box = useRef<HTMLDivElement>(null);
  // Held in a ref so the open/close effect runs once per mount even though the
  // parent passes a fresh closure on every render.
  const close = useRef(onClose);
  close.current = onClose;

  useEffect(() => setUndecodable(false), [src]);

  useEffect(() => {
    const opener = document.activeElement as HTMLElement | null;
    const scrollLock = document.body.style.overflow;
    document.body.style.overflow = 'hidden';
    box.current?.focus();
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') close.current();
    };
    window.addEventListener('keydown', onKey);
    return () => {
      window.removeEventListener('keydown', onKey);
      document.body.style.overflow = scrollLock;
      opener?.focus();
    };
  }, []);

  return (
    <div
      ref={box}
      role="dialog"
      aria-modal="true"
      aria-label={`放大查看 ${label}`}
      tabIndex={-1}
      className="fixed inset-0 z-50 flex flex-col bg-ink-950/95 outline-none"
    >
      <div className="safe-top flex shrink-0 items-center gap-2 border-b border-ink-700 px-3 py-2 text-xs text-ink-400">
        <span className="tabular-nums">{label}</span>
        <button
          type="button"
          disabled={!natural}
          onClick={() => setActual((v) => !v)}
          className="rounded border border-ink-600 px-2 py-1 text-ink-400 hover:border-ink-400 disabled:opacity-40"
        >
          {actual ? '适应屏幕' : `100%${natural ? ` · ${natural[0]}×${natural[1]}` : ''}`}
        </button>
        <a
          href={src}
          download={name}
          className="rounded border border-ink-600 px-2 py-1 text-ink-400 hover:border-ink-400"
        >
          另存
        </a>
        <button
          type="button"
          onClick={onClose}
          className="ml-auto rounded border border-ink-600 px-2 py-1 text-ink-400 hover:border-ink-400"
        >
          关闭
        </button>
      </div>
      <div onClick={onClose} className="min-h-0 flex-1 overflow-auto">
        {undecodable ? (
          <p className="m-auto max-w-sm px-4 pt-24 text-center text-sm text-ink-400">
            浏览器画不出 {name.slice(name.lastIndexOf('.') + 1).toUpperCase()}，用「另存」拿到这一张。
          </p>
        ) : (
          <img
            src={src}
            alt={label}
            onError={() => setUndecodable(true)}
            onClick={(e) => {
              e.stopPropagation();
              setActual((v) => !v);
            }}
            onLoad={(e) => {
              const img = e.currentTarget;
              setNatural([img.naturalWidth, img.naturalHeight]);
            }}
            className="m-auto block select-none"
            style={
              actual && natural
                ? { width: natural[0], height: natural[1], maxWidth: 'none', maxHeight: 'none' }
                : { maxWidth: '100%', maxHeight: '100%' }
            }
          />
        )}
      </div>
    </div>
  );
}
