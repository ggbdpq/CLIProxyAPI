import { z } from 'zod'

/** 数据记录：data 为导入 JSONL 的原始对象（结构不固定，表格动态列来源） */
export const DataRecordSchema = z.object({
  id: z.number(),
  source_file: z.string(),
  line_number: z.number(),
  summary: z.record(z.string(), z.unknown()).optional(),
  data: z.unknown(),
  imported_at: z.string(),
  token_exp: z.number().optional(),
})
export type DataRecord = z.infer<typeof DataRecordSchema>

export const RecordsListResponseSchema = z.object({
  total: z.number(),
  records: z.array(DataRecordSchema),
})

export const StatsResponseSchema = z.object({
  total: z.number(),
  lifecycle: z.record(z.string(), z.number()),
  health: z.record(z.string(), z.number()),
  auth_state: z.record(z.string(), z.number()),
  detected: z.number().optional().default(0),
})
export type StatsResponse = z.infer<typeof StatsResponseSchema>

/** 批次条目：健康/授权分布为动态计数 map，其余字段可缺省 */
export const BatchSchema = z.object({
  batch_key: z.string(),
  batch_date: z.string().optional(),
  order_id: z.string().optional(),
  quantity: z.number().nullable().optional(),
  total_cost: z.union([z.string(), z.number()]).optional(),
  record_count: z.number().optional(),
  health: z.record(z.string(), z.number()).optional(),
  auth_state: z.record(z.string(), z.number()).optional(),
  order_url: z.string().optional(),
  notes: z.string().optional(),
})
export type Batch = z.infer<typeof BatchSchema>

export const BatchesResponseSchema = z.object({
  total: z.number(),
  batches: z.array(BatchSchema),
})

export const DeleteResponseSchema = z.object({ deleted: z.number() })
export const UpdateResponseSchema = z.object({ updated: z.number() })
export const GenerateQuotaResponseSchema = z.object({
  exported: z.number(),
  output_dir: z.string(),
  files: z.array(z.string()),
})
export type GenerateQuotaResult = z.infer<typeof GenerateQuotaResponseSchema>
export const DeployResponseSchema = z.object({
  deployed: z.number(),
  output_dir: z.string(),
  target: z.string().optional(),
  files: z.array(z.string()),
})
export const RecycleResponseSchema = z.object({
  recycled: z.number(),
  output_dir: z.string(),
  target: z.string().optional(),
  files: z.array(z.string()),
})
export const RefreshTokenResponseSchema = z.object({
  ok: z.boolean(),
  email: z.string().optional(),
  expires_in: z.number().optional(),
})
export const ConvertResponseSchema = z.object({
  from: z.string(),
  to: z.string(),
  filename: z.string(),
  converted: z.number(),
  content: z.string(),
})
export type ConvertResult = z.infer<typeof ConvertResponseSchema>
export const RegisterReauthResponseSchema = z.object({
  total: z.number(),
  updated: z.number(),
  imported: z.number(),
  unmatched: z.number(),
  missing_email: z.number(),
  missing_token: z.number(),
  skipped: z.number(),
})
export type RegisterReauthResult = z.infer<typeof RegisterReauthResponseSchema>
export const ImportResponseSchema = z.object({
  status: z.string(),
  imported: z.number(),
  stats: z
    .object({
      imported: z.number(),
      replaced_existing: z.number(),
      enriched_existing: z.number(),
      skipped_existing: z.number(),
      deduped_within_file: z.number(),
      batches_registered: z.number(),
    })
    .optional(),
})

export const LIFECYCLE_LABELS = {
  unused: '未用',
  in_use: '使用中',
  sold: '已售出',
  archived: '归档',
} as const

export const HEALTH_LABELS = {
  ok: '正常',
  unknown: '未测',
  depleted: '已耗尽',
  token_expired: 'Token过期',
  token_invalidated: 'Token吊销',
  token_invalid: 'Token无效',
  workspace_deactivated: '工作台停用',
  err: '错误',
} as const

/** 配额检测走的 ChatGPT 用量端点（经后端 /v0/management/api-call 代理） */
export const WHAM_USAGE_URL = 'https://chatgpt.com/backend-api/wham/usage'

/** 检测目标提取：email 与 access_token 的候选字段（与旧版一致） */
export function recordEmail(data: unknown): string {
  if (!data || typeof data !== 'object') return ''
  const obj = data as Record<string, unknown>
  for (const key of ['email', 'account_claims_email', 'login_identity', 'account_email']) {
    const value = obj[key]
    if (typeof value === 'string' && value.trim()) return value.trim()
  }
  return ''
}

export function recordToken(data: unknown): string {
  if (!data || typeof data !== 'object') return ''
  const value = (data as Record<string, unknown>).access_token
  return typeof value === 'string' ? value.trim() : ''
}

export function recordRefreshToken(data: unknown): string {
  if (!data || typeof data !== 'object') return ''
  const value = (data as Record<string, unknown>).refresh_token
  return typeof value === 'string' ? value.trim() : ''
}

/** 动态列：从当前页 records 的 data 收集（quota/status/nextTime 优先前置） */
export function dataColumns(records: DataRecord[]): string[] {
  const seen = new Set<string>()
  const columns: string[] = []
  for (const record of records) {
    const data = record.data
    if (!data || typeof data !== 'object' || Array.isArray(data)) {
      if (!seen.has('value')) {
        seen.add('value')
        columns.push('value')
      }
      continue
    }
    for (const key of Object.keys(data)) {
      if (!seen.has(key)) {
        seen.add(key)
        columns.push(key)
      }
    }
  }
  const priority: string[] = []
  for (const key of ['quota', 'status', 'nextTime']) {
    const index = columns.indexOf(key)
    if (index !== -1) priority.push(columns.splice(index, 1)[0])
  }
  return [...priority, ...columns]
}

export function formatCellValue(value: unknown, key: string): string {
  if (value === undefined || value === null) return ''
  if (key === 'quota') return formatQuotaValue(value)
  if (key === 'nextTime') return formatNextTimeValue(value)
  if (typeof value === 'object') return JSON.stringify(value, null, 2)
  return String(value)
}

function formatQuotaValue(value: unknown): string {
  const text = String(value).trim()
  if (!text) return ''
  if (/%$/.test(text)) return text
  const number = Number(text)
  return Number.isFinite(number) ? `${number}%` : text
}

function formatNextTimeValue(value: unknown): string {
  const text = String(value).trim()
  if (!text) return ''
  if (/^\d{1,2}-\d{1,2}$/.test(text)) return `${text} 00:00`
  if (/^\d{1,2}-\d{1,2}\s+\d{1,2}:\d{1,2}$/.test(text)) {
    return text.replace(/(\d{1,2}):(\d{1,2})$/, (_, h: string, m: string) => `${h.padStart(2, '0')}:${m.padStart(2, '0')}`)
  }
  return text
}
