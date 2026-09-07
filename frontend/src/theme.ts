export type Theme = 'light' | 'dark';

const authenticatedThemeKey = 'proxyma_authenticated_theme';

export function normalizeTheme(value: unknown): Theme {
  return value === 'dark' ? 'dark' : 'light';
}

export function applyTheme(theme: Theme) {
  document.documentElement.dataset.theme = theme;
}

export function setAuthenticatedTheme(theme: Theme) {
  window.sessionStorage.setItem(authenticatedThemeKey, theme);
  applyTheme(theme);
}

export function clearAuthenticatedTheme() {
  window.sessionStorage.removeItem(authenticatedThemeKey);
  applyTheme('light');
}

export function initializeTheme(hasSession: boolean) {
  applyTheme(hasSession ? normalizeTheme(window.sessionStorage.getItem(authenticatedThemeKey)) : 'light');
}
