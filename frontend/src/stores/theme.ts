import { defineStore } from 'pinia'
import { ref, watch } from 'vue'

export type Theme = 'light' | 'dark'

export const useThemeStore = defineStore('theme', () => {
  const stored = localStorage.getItem('nb-theme') as Theme | null
  const theme = ref<Theme>(stored ?? 'light')

  function applyTheme(t: Theme) {
    const root = document.documentElement
    if (t === 'dark') {
      root.classList.add('dark')
    } else {
      root.classList.remove('dark')
    }
  }

  function toggleTheme() {
    theme.value = theme.value === 'light' ? 'dark' : 'light'
  }

  function setTheme(t: Theme) {
    theme.value = t
  }

  watch(theme, (t) => {
    applyTheme(t)
    localStorage.setItem('nb-theme', t)
  }, { immediate: true })

  return { theme, toggleTheme, setTheme }
})
