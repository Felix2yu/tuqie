const UNITS = ['KB', 'MB', 'GB', 'TB'];

/**
 * Words a byte count the way the operator typed it into the config, so the number
 * in the hint is that number and not one the bundle remembers. Units are 1024s,
 * matching the server's own formatter.
 */
export function humanBytes(n: number): string {
  if (n < 1024) return `${n} B`;
  let div = 1024;
  let name = 'KB';
  for (let i = 1; i < UNITS.length; i++) {
    const next = 1024 ** (i + 1);
    if (n < next) break;
    div = next;
    name = UNITS[i];
  }
  const v = n / div;
  return `${Number.isInteger(v) ? v : v.toFixed(1)} ${name}`;
}
