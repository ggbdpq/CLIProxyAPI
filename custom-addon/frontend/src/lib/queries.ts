import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'

import { apiFetch } from '@/lib/api'
import {
  BatchesResponseSchema,
  ConvertResponseSchema,
  DeleteResponseSchema,
  DeployResponseSchema,
  GenerateQuotaResponseSchema,
  ImportResponseSchema,
  RecordsListResponseSchema,
  RegisterReauthResponseSchema,
  RecycleResponseSchema,
  RefreshTokenResponseSchema,
  StatsResponseSchema,
  UpdateResponseSchema,
  type StatsResponse,
} from '@/lib/schemas'
import { useFilters } from '@/store/app'

export const queryKeys = {
  records: (params: Record<string, string | number>) => ['records', params] as const,
  stats: ['records', 'stats'] as const,
  batches: ['batches'] as const,
}

export function useRecordsQuery() {
  const { q, lifecycle, health, authState, batch, detected, page, pageSize } = useFilters()
  const params: Record<string, string | number> = {
    limit: pageSize,
    offset: (page - 1) * pageSize,
  }
  if (q) params.q = q
  if (lifecycle) params.lifecycle = lifecycle
  if (health) params.health = health
  if (authState) params.auth_state = authState
  if (batch) params.batch = batch
  if (detected) params.detected = 1

  return useQuery({
    queryKey: queryKeys.records(params),
    queryFn: async () => {
      const search = new URLSearchParams()
      for (const [key, value] of Object.entries(params)) search.set(key, String(value))
      const raw = await apiFetch<unknown>(`/v0/management/data-records?${search.toString()}`)
      return RecordsListResponseSchema.parse(raw)
    },
  })
}

export function useStatsQuery(): { data: StatsResponse | undefined; abnormal: number } {
  const query = useQuery({
    queryKey: queryKeys.stats,
    queryFn: async () => StatsResponseSchema.parse(await apiFetch<unknown>('/v0/management/data-records/stats')),
  })
  const health = query.data?.health ?? {}
  const abnormal = Object.entries(health)
    .filter(([key]) => key !== 'ok' && key !== 'unknown')
    .reduce((sum, [, count]) => sum + count, 0)
  return { data: query.data, abnormal }
}

export function useBatchesQuery() {
  return useQuery({
    queryKey: queryKeys.batches,
    queryFn: async () => BatchesResponseSchema.parse(await apiFetch<unknown>('/v0/management/data-records/batches')),
  })
}

function useInvalidateRecords() {
  const qc = useQueryClient()
  return () => {
    void qc.invalidateQueries({ queryKey: ['records'] })
    void qc.invalidateQueries({ queryKey: queryKeys.batches })
  }
}

export function useDeleteRecordsMutation() {
  const invalidate = useInvalidateRecords()
  const clearSelected = useFilters((s) => s.clearSelected)
  return useMutation({
    mutationFn: async (input: { ids?: number[]; all?: boolean }) => {
      const raw = await apiFetch<unknown>('/v0/management/data-records', {
        method: 'DELETE',
        body: JSON.stringify(input.all ? { all: true } : { ids: input.ids ?? [] }),
      })
      const parsed = DeleteResponseSchema.parse(raw)
      clearSelected()
      return parsed
    },
    onSuccess: invalidate,
  })
}

export function useUpdateStateMutation() {
  const invalidate = useInvalidateRecords()
  return useMutation({
    mutationFn: async (input: { ids: number[]; lifecycle: string }) =>
      UpdateResponseSchema.parse(
        await apiFetch<unknown>('/v0/management/data-records/update-state', {
          method: 'POST',
          body: JSON.stringify(input),
        }),
      ),
    onSuccess: invalidate,
  })
}

export function useUpdateBatchMutation() {
  const invalidate = useInvalidateRecords()
  return useMutation({
    mutationFn: async (input: { batch_key: string; order_url: string; notes: string }) =>
      apiFetch<unknown>('/v0/management/data-records/update-batch', {
        method: 'POST',
        body: JSON.stringify(input),
      }),
    onSuccess: invalidate,
  })
}

export function useGenerateQuotaMutation() {
  return useMutation({
    mutationFn: async (ids: number[]) =>
      GenerateQuotaResponseSchema.parse(
        await apiFetch<unknown>('/v0/management/data-records/generate-quota', {
          method: 'POST',
          body: JSON.stringify({ ids }),
        }),
      ),
  })
}

export function useDeployRecordsMutation() {
  const invalidate = useInvalidateRecords()
  return useMutation({
    mutationFn: async (input: { ids: number[]; target: string }) =>
      DeployResponseSchema.parse(
        await apiFetch<unknown>('/v0/management/data-records/deploy', {
          method: 'POST',
          body: JSON.stringify(input),
        }),
      ),
    onSuccess: invalidate,
  })
}

export function useRecycleRecordsMutation() {
  const invalidate = useInvalidateRecords()
  return useMutation({
    mutationFn: async (input: { ids: number[]; target: string }) =>
      RecycleResponseSchema.parse(
        await apiFetch<unknown>('/v0/management/data-records/recycle', {
          method: 'POST',
          body: JSON.stringify(input),
        }),
      ),
    onSuccess: invalidate,
  })
}

export function useRefreshTokenMutation() {
  const invalidate = useInvalidateRecords()
  return useMutation({
    mutationFn: async (id: number) =>
      RefreshTokenResponseSchema.parse(
        await apiFetch<unknown>('/v0/management/data-records/refresh-token', {
          method: 'POST',
          body: JSON.stringify({ id }),
        }),
      ),
    onSettled: invalidate,
  })
}

export async function convertDataRecords(input: { from: string; to: string; content: string }) {
  return ConvertResponseSchema.parse(
    await apiFetch<unknown>('/v0/management/data-records/convert', {
      method: 'POST',
      body: JSON.stringify(input),
    }),
  )
}

export function useRegisterReauthMutation() {
  const invalidate = useInvalidateRecords()
  return useMutation({
    mutationFn: async (input: { file: File; importUnmatched: boolean }) => {
      const form = new FormData()
      form.append('file', input.file)
      const flag = input.importUnmatched ? '1' : '0'
      return RegisterReauthResponseSchema.parse(
        await apiFetch<unknown>(`/v0/management/data-records/register-reauth?import_unmatched=${flag}`, {
          method: 'POST',
          body: form,
        }),
      )
    },
    onSuccess: invalidate,
  })
}

export function useImportMutation() {
  const invalidate = useInvalidateRecords()
  const resetPage = useFilters((s) => s.resetPage)
  return useMutation({
    mutationFn: async (file: File) => {
      const form = new FormData()
      form.append('file', file)
      const raw = await apiFetch<unknown>('/v0/management/data-records/import?dedupe=1', {
        method: 'POST',
        body: form,
      })
      return ImportResponseSchema.parse(raw)
    },
    onSuccess: () => {
      resetPage()
      invalidate()
    },
  })
}

/** 导出选中记录为 JSONL（blob 下载，需绕过 apiFetch 的 JSON 解析） */
export async function exportRecordsJSONL(ids: number[]): Promise<void> {
  const key = localStorage.getItem('cpa-data-management-key') ?? ''
  const resp = await fetch(`/v0/management/data-records/export?ids=${ids.join(',')}`, {
    headers: { Authorization: `Bearer ${key}` },
  })
  if (!resp.ok) throw new Error(`导出失败：${resp.status}`)
  const blob = await resp.blob()
  const url = URL.createObjectURL(blob)
  const anchor = document.createElement('a')
  anchor.href = url
  anchor.download = 'data-records.jsonl'
  document.body.appendChild(anchor)
  anchor.click()
  anchor.remove()
  URL.revokeObjectURL(url)
}

/** 按 200/页收集当前筛选下的全部记录（检测「筛选结果」用，上限 5000） */
export async function collectFilteredRecords(
  filterQS: string,
): Promise<import('./schemas').DataRecord[]> {
  const collected: import('./schemas').DataRecord[] = []
  const pageLimit = 200
  let offset = 0
  for (;;) {
    const raw = await apiFetch<unknown>(
      `/v0/management/data-records?limit=${pageLimit}&offset=${offset}${filterQS}`,
    )
    const data = RecordsListResponseSchema.parse(raw)
    collected.push(...data.records)
    if (
      data.records.length < pageLimit ||
      collected.length >= data.total ||
      collected.length >= 5000
    ) {
      break
    }
    offset += pageLimit
  }
  return collected
}
