import { defineStore } from 'pinia'
import { ref } from 'vue'
import axios from 'axios'

const TOKEN_KEY = 'nb_token'
const USER_ID_KEY = 'nb_user_id'
const USERNAME_KEY = 'nb_username'
const ROLE_KEY = 'nb_role'

type SessionPayload = {
  user_id?: string
  username?: string
  role?: string
}

export const useAuthStore = defineStore('auth', () => {
  const token = ref<string>(localStorage.getItem(TOKEN_KEY) ?? '')
  const userId = ref<string>(localStorage.getItem(USER_ID_KEY) ?? '')
  const username = ref<string>(localStorage.getItem(USERNAME_KEY) ?? '')
  const role = ref<string>(localStorage.getItem(ROLE_KEY) ?? '')
  const checked = ref(false)

  function setToken(t: string) {
    token.value = t
    if (t) {
      localStorage.setItem(TOKEN_KEY, t)
    } else {
      localStorage.removeItem(TOKEN_KEY)
    }
  }

  function setSession(payload: SessionPayload) {
    userId.value = payload.user_id ?? ''
    username.value = payload.username ?? ''
    role.value = payload.role ?? ''
    if (userId.value) localStorage.setItem(USER_ID_KEY, userId.value)
    else localStorage.removeItem(USER_ID_KEY)
    if (username.value) localStorage.setItem(USERNAME_KEY, username.value)
    else localStorage.removeItem(USERNAME_KEY)
    if (role.value) localStorage.setItem(ROLE_KEY, role.value)
    else localStorage.removeItem(ROLE_KEY)
  }

  async function login(user: string, password: string) {
    const res = await axios.post('/api/auth/login', { username: user, password })
    setToken(res.data.token)
    setSession(res.data)
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
    setSession({})
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
      setSession(res.data)
      checked.value = true
      return true
    } catch {
      setToken('')
      setSession({})
      checked.value = true
      return false
    }
  }

  return { token, userId, username, role, checked, login, logout, check, setToken, setSession }
})
