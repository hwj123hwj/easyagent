export type ThemeMode = 'system' | 'light' | 'dark';

const STORAGE_KEY = 'pigo.theme';

export function loadStoredTheme(): ThemeMode {
  try {
    const v = localStorage.getItem(STORAGE_KEY);
    if (v === 'system' || v === 'light' || v === 'dark') return v;
  } catch {
    /* localStorage unavailable */
  }
  return 'dark';
}

export function persistTheme(mode: ThemeMode): void {
  try {
    localStorage.setItem(STORAGE_KEY, mode);
  } catch {
    /* best-effort */
  }
}

export function applyTheme(mode: ThemeMode): void {
  document.documentElement.setAttribute('data-theme', mode);
}
