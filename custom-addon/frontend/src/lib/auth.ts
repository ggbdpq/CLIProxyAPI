const KEY_STORAGE = 'cpa-data-management-key'

/** 读取本机持久化的管理密钥（仅存浏览器 localStorage，不外发） */
export function getManagementKey(): string {
  return localStorage.getItem(KEY_STORAGE) ?? ''
}

export function setManagementKey(value: string): void {
  const trimmed = value.trim()
  if (trimmed) localStorage.setItem(KEY_STORAGE, trimmed)
  else localStorage.removeItem(KEY_STORAGE)
}

export function hasManagementKey(): boolean {
  return getManagementKey().trim() !== ''
}

/** 401 时清除密钥，路由守卫会在下次导航时弹回登录页 */
export function clearManagementKey(): void {
  localStorage.removeItem(KEY_STORAGE)
}
