import axios from 'axios'
import router from '@/router'

const TOKEN_KEY = 'ohmysms_token'

const client = axios.create({
  baseURL: '/api',
  timeout: 15_000,
  headers: {
    'Content-Type': 'application/json',
  },
})

// ─── 请求拦截：自动注入 JWT ───
client.interceptors.request.use((config) => {
  const token = localStorage.getItem(TOKEN_KEY)
  if (token) {
    config.headers.Authorization = `Bearer ${token}`
  }
  return config
})

// ─── 响应拦截：401 自动跳登录 ───
client.interceptors.response.use(
  (response) => response,
  (error) => {
    if (error.response?.status === 401) {
      localStorage.removeItem(TOKEN_KEY)
      router.push({ name: 'login' })
    }
    return Promise.reject(error)
  },
)

export { TOKEN_KEY }
export default client
