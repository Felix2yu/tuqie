import { setChoice, useTheme, type ThemeChoice } from '../lib/theme';

const CHOICES: { id: ThemeChoice; label: string; hint: string }[] = [
  { id: 'auto', label: '自动', hint: '跟随系统的亮暗设置' },
  { id: 'light', label: '亮', hint: '一直用亮色' },
  { id: 'dark', label: '暗', hint: '一直用暗色' },
];

export default function ThemeToggle() {
  const { choice } = useTheme();
  return (
    <div
      role="radiogroup"
      aria-label="主题"
      className="flex shrink-0 overflow-hidden rounded-lg border border-edge bg-sunken text-xs"
    >
      {CHOICES.map((c) => (
        <button
          key={c.id}
          type="button"
          role="radio"
          aria-checked={choice === c.id}
          title={c.hint}
          onClick={() => setChoice(c.id)}
          className={`px-2 py-1.5 ${choice === c.id ? 'bg-accent text-on-accent' : 'text-muted hover:text-ink'}`}
        >
          {c.label}
        </button>
      ))}
    </div>
  );
}
