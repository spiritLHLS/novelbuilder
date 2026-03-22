import { defineStore } from 'pinia'
import { ref } from 'vue'
import axios from 'axios'

const TOKEN_KEY = 'nb_token'

export const useAuthStore = defineStore('auth', () => {
  const token = ref<string>(localStorage.getItem(TOKEN_KEY) ?? '')
  const username = ref<string>('')
  const checked = ref(false)

  function setToken(t: string) {
    token.value = t
    if (t) {
      localStorage.setItem(TOKEN_KEY, t)
    } else {
      localStorage.removeItem(TOKEN_KEY)
    }
  }

  async function login(user: string, password: string) {
    const res = await axios.post('/api/auth/login', { username: user, password })
    setToken(res.data.token)
    username.value = res.data.username
    checked.value = true
  }

  async function logout() {
    try {
      await axios.post('/api/auth/logout', {}, {
        headers: { Authorization: `Bearer ${token.value}` },
      })
    } catch {
      // ignore errors on logout
    }
    setToken('')
    username.value = ''
    checked.value = false
  }

  async function check(): Promise<boolean> {
    if (!token.value) {
      checked.value = true
      return false
    }
    try {
      const res = await axios.get('/api/auth/check', {
        headers: { Authorization: `Bearer ${token.value}` },
      })
      username.value = res.data.username ?? ''
      checked.value = true
      return true
    } catch {
      setToken('')
      checked.value = true
      return false
    }
  }

  return { token, username, checked, login, logout, check, setToken }
})
