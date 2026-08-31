import { create } from 'zustand'

import { apiFetch } from '@/lib/api'
import { WHAM_USAGE_URL, recordEmail, recordToken, type DataRecord } from '@/lib/schemas'

/** 列表筛选/分页/选中集合（跨组件共享） */
interface FiltersState {
  q: string
  lifecycle: string
  health: string
  authState: string
  batch: string
  page: number
  pageSize: number
  selected: Record<string, true>
  setQ: (q: string) => void
  setLifecycle: (v: string) => void
  setHealth: (v: string) => void
  setAuthState: (v: string) => void
  setBatch: (v: string) => void
  resetPage: () => void
  goToPage: (delta: number) => void
  toggleSelected: (id: string, checked: boolean) => void
  selectAll: (ids: string[], checked: boolean) => void
  pruneSelected: (ids: string[]) => void
  clearSelected: () => void
  selectedIds: () => number[]
}

export const useFilters = create<FiltersState>((set, get) => ({
  q: '',
  lifecycle: '',
  health: '',
  authState: '',
  batch: '',
  page: 1,
  pageSize: 50,
  selected: {},
  setQ: (q) => set({ q, page: 1 }),
  setLifecycle: (lifecycle) => set({ lifecycle, page: 1 }),
  setHealth: (health) => set({ health, page: 1 }),
  setAuthState: (authState) => set({ authState, page: 1 }),
  setBatch: (batch) => set({ batch, page: 1 }),
  resetPage: () => set({ page: 1 }),
  goToPage: (delta) => set((s) => ({ page: Math.max(1, s.page + delta) })),
  toggleSelected: (id, checked) =>
    set((s) => {
      const next = { ...s.selected }
      if (checked) next[id] = true
      else delete next[id]
      return { selected: next }
    }),
  selectAll: (ids, checked) =>
    set(() => {
      const next: Record<string, true> = {}
      if (checked) for (const id of ids) next[id] = true
      return { selected: next }
    }),
  pruneSelected: (ids) =>
    set((s) => {
      const next: Record<string, true> = {}
      for (const id of ids) if (s.selected[id]) next[id] = true
      return { selected: next }
    }),
  clearSelected: () => set({ selected: {} }),
  selectedIds: () =>
    Object.keys(get().selected)
      .map((id) => Number.parseInt(id, 10))
      .filter((id) => Number.isSafeInteger(id) && id > 0),
}))

/** 配额检测：全局 store 保证翻页/切筛选时循环持续运行 */
interface DetectState {
  running: boolean
  abort: boolean
  done: number
  total: number
  ok: number
  failed: number
  skipped: number
  note: string
}

interface DetectionStore extends DetectState {
  start: (targets: { email: string; token: string }[]) => Promise<void>
  stop: () => void
  reset: () => void
}

const DETECT_INTERVAL_MS = 3000
const DETECT_RATE_LIMIT_PAUSE_MS = 60000

const sleep = (ms: number) => new Promise((resolve) => setTimeout(resolve, ms))

export const useDetection = create<DetectionStore>((set, get) => ({
  running: false,
  abort: false,
  done: 0,
  total: 0,
  ok: 0,
  failed: 0,
  skipped: 0,
  note: '',
  stop: () => set({ abort: true, note: '正在停止...' }),
  reset: () =>
    set({ running: false, abort: false, done: 0, total: 0, ok: 0, failed: 0, skipped: 0, note: '' }),
  start: async (targets) => {
    if (get().running) return
    set({ running: true, abort: false, done: 0, total: targets.length, ok: 0, failed: 0, skipped: 0, note: '' })
    for (let i = 0; i < targets.length; i++) {
      if (get().abort) {
        set({ note: '已手动停止' })
        break
      }
      const target = targets[i]
      set((s) => ({ done: s.done + 1, note: `正在检测 ${target.email}` }))
      try {
        const resp = await apiFetch<{ status_code?: number }>('/v0/management/api-call', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json', 'X-Cpa-Data-Email': target.email },
          body: JSON.stringify({
            method: 'GET',
            url: WHAM_USAGE_URL,
            header: { Authorization: `Bearer ${target.token}` },
          }),
        })
        const code = typeof resp?.status_code === 'number' ? resp.status_code : 0
        if (code === 429) {
          set((s) => ({ skipped: s.skipped + 1 }))
          for (let wait = DETECT_RATE_LIMIT_PAUSE_MS / 1000; wait > 0; wait -= 5) {
            if (get().abort) break
            set({ note: `触发限流（429），退避 ${wait} 秒后继续...` })
            await sleep(5000)
          }
        } else if (code >= 200 && code < 300) {
          set((s) => ({ ok: s.ok + 1 }))
        } else {
          set((s) => ({ failed: s.failed + 1 }))
        }
      } catch {
        set((s) => ({ failed: s.failed + 1 }))
      }
      set({ note: '' })
      if (i < targets.length - 1) await sleep(DETECT_INTERVAL_MS)
    }
    const s = get()
    set({
      running: false,
      note: `检测结束：正常 ${s.ok}，失败 ${s.failed}，跳过 ${s.skipped}${s.abort ? '（手动停止）' : ''}`,
    })
  },
}))

/** 从记录池提取可检测目标（email + access_token 齐全） */
export function detectTargetsFrom(pool: DataRecord[]): { email: string; token: string }[] {
  return pool
    .filter((record) => recordEmail(record.data) && recordToken(record.data))
    .map((record) => ({ email: recordEmail(record.data), token: recordToken(record.data) }))
}
