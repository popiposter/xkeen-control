import { useEffect, useId, useState } from 'react'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { Field, FieldLabel } from '@/components/ui/field'
import { THEME_KEY, applyTheme, readTheme } from './theme-init'
export function ThemeControl() {
  const id = useId()
  const [theme, setTheme] = useState(readTheme)
  useEffect(() => {
    const media = window.matchMedia('(prefers-color-scheme: dark)')
    const refresh = () => applyTheme(theme)
    refresh(); media.addEventListener('change', refresh)
    const sync = (event) => { if (event.key === THEME_KEY || event.key === null) setTheme(readTheme()) }
    window.addEventListener('storage', sync)
    return () => { media.removeEventListener('change', refresh); window.removeEventListener('storage', sync) }
  }, [theme])
  return <Field><FieldLabel htmlFor={id}>Appearance</FieldLabel><NativeSelect id={id} aria-label="Appearance" value={theme} onChange={(event) => { const value = event.target.value; setTheme(value); try { localStorage.setItem(THEME_KEY, value) } catch { /* Theme still works without storage. */ } }}><NativeSelectOption value="system">System</NativeSelectOption><NativeSelectOption value="light">Light</NativeSelectOption><NativeSelectOption value="dark">Dark</NativeSelectOption></NativeSelect></Field>
}
