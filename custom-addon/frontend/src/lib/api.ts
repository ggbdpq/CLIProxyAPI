import { clearManagementKey, getManagementKey } from '@/lib/auth'

/** API 错误：携带 HTTP 状态码，401 时已自动清除本地密钥 */
export class ApiError extends Error {
  status: number

  constructor(message: string, status: number) {
    super(message)
    this.name = 'ApiError'
    this.status = status
  }
}

/** 统一 API 出口：注入 Bearer 管理密钥，401 自动清密钥（路由守卫弹回登录页） */
export async function apiFetch<T>(url: string, options: RequestInit = {}): Promise<T> {
  const headers = new Headers(options.headers)
  const key = getManagementKey()
  if (key) headers.set('Authorization', `Bearer ${key}`)
  if (typeof options.body === 'string') headers.set('Content-Type', 'application/json')

  const resp = await fetch(url, { ...options, headers })
  const text = await resp.text()
  const data: unknown = text ? JSON.parse(text) : {}

  if (!resp.ok) {
    if (resp.status === 401) clearManagementKey()
    const message =
      typeof data === 'object' && data !== null && 'error' in data && typeof data.error === 'string'
        ? data.error
        : typeof data === 'object' && data !== null && 'message' in data && typeof data.message === 'string'
          ? data.message
          : `请求失败：${resp.status}`
    throw new ApiError(message, resp.status)
  }
  return data as T
}
