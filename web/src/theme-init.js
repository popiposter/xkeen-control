export const THEME_KEY = 'xkeen-ui-theme-v1'
export function readTheme() {
  try { const value = localStorage.getItem(THEME_KEY); return ['light', 'dark', 'system'].includes(value) ? value : 'system' } catch { return 'system' }
}
export function applyTheme(theme) {
  const dark = theme === 'dark' || theme === 'system' && window.matchMedia('(prefers-color-scheme: dark)').matches
  document.documentElement.classList.toggle('dark', dark)
  document.documentElement.dataset.theme = theme
}
applyTheme(readTheme())
