import { useSyncExternalStore } from 'react';

export type ThemeChoice = 'auto' | 'light' | 'dark';
export type Theme = 'light' | 'dark';

const KEY = 'tuqie-theme';
const QUERY = '(prefers-color-scheme: dark)';

// Read once and kept: the media query is what makes 'auto' mean anything, and it
// is asked for on every render.
const dark = matchMedia(QUERY);

function isChoice(value: unknown): value is ThemeChoice {
  return value === 'auto' || value === 'light' || value === 'dark';
}

function stored(): ThemeChoice {
  try {
    const value = localStorage.getItem(KEY);
    return isChoice(value) ? value : 'auto';
  } catch {
    // A private window can refuse storage outright; the theme still has to work.
    return 'auto';
  }
}

export function resolve(choice: ThemeChoice): Theme {
  return choice === 'auto' ? (dark.matches ? 'dark' : 'light') : choice;
}

/**
 * Put the resolved theme on the document, which is the only thing CSS then needs.
 *
 * The browser chrome — the iOS status bar, the address bar — is not styled by the
 * page, so it takes its colour from a meta tag that has to be rewritten by hand.
 * It copies the palette's own surface rather than naming a second hex, or the two
 * would drift apart on the next colour change.
 */
export function paint(choice: ThemeChoice): Theme {
  const theme = resolve(choice);
  document.documentElement.dataset.theme = theme;
  const meta = document.querySelector('meta[name="theme-color"]');
  if (meta) {
    const surface = getComputedStyle(document.documentElement).getPropertyValue('--p-surface').trim();
    if (surface) meta.setAttribute('content', surface);
  }
  return theme;
}

const listeners = new Set<() => void>();
let choice = stored();

function announce() {
  for (const l of listeners) l();
}

// 'auto' follows the system for as long as the page is open, not only as far as
// its own load: the OS can change theme underneath it.
dark.addEventListener('change', () => {
  if (choice !== 'auto') return;
  paint(choice);
  announce();
});

export function setChoice(next: ThemeChoice) {
  if (next === choice) return;
  choice = next;
  try {
    localStorage.setItem(KEY, next);
  } catch {
    /* it holds for this visit, which is all a refusal can cost */
  }
  paint(next);
  announce();
}

// The bundle loads a moment after the page is first painted; index.html has
// already set data-theme so there is nothing to undo, only the meta tag left to
// bring in line.
paint(choice);

function subscribe(listener: () => void) {
  listeners.add(listener);
  return () => void listeners.delete(listener);
}

export function useTheme(): {
  choice: ThemeChoice;
  theme: Theme;
  setChoice: (next: ThemeChoice) => void;
} {
  const selected = useSyncExternalStore(subscribe, () => choice);
  const applied = useSyncExternalStore(subscribe, () => resolve(choice));
  return { choice: selected, theme: applied, setChoice };
}
