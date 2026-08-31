import { createFileRoute, useNavigate } from '@tanstack/react-router'
import { useEffect, useState } from 'react'

import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { HEALTH_LABELS } from '@/lib/schemas'
import { useBatchesQuery, useUpdateBatchMutation } from '@/lib/queries'
import { useFilters } from '@/store/app'

/** 批次短标签：'12.xxx' → '批次 12'，其余原样 */
function shortBatchLabel(key: string): string {
  const match = /^(\d{1,4})\./.exec(key ?? '')
  return match ? `批次 ${match[1]}` : key ?? ''
}

function healthText(health: Record<string, number>): { text: string; bad: boolean } {
  const text =
    Object.entries(health)
      .map(([key, count]) => `${HEALTH_LABELS[key as keyof typeof HEALTH_LABELS] ?? key} ${count}`)
      .join('，') || '-'
  const bad =
    (health.token_expired ?? 0) +
    (health.token_invalidated ?? 0) +
    (health.token_invalid ?? 0) +
    (health.workspace_deactivated ?? 0) +
    (health.err ?? 0) +
    (health.depleted ?? 0)
  return { text, bad: bad > 0 }
}

function authText(auth: Record<string, number>): string {
  return (
    Object.entries(auth)
      .map(([key, count]) => (key === 'authorized' ? `已授权 ${count}` : `缺mailapi ${count}`))
      .join('，') || '-'
  )
}

interface BatchDraft {
  order_url: string
  notes: string
}

function BatchesPage() {
  const navigate = useNavigate()
  const batchesQuery = useBatchesQuery()
  const updateBatch = useUpdateBatchMutation()
  const setBatch = useFilters((s) => s.setBatch)
  const [drafts, setDrafts] = useState<Record<string, BatchDraft>>({})
  const [feedback, setFeedback] = useState<{ error?: string; message?: string }>({})

  const batches = batchesQuery.data?.batches ?? []

  // 服务端数据到达/刷新后，同步行内编辑草稿
  useEffect(() => {
    setDrafts((prev) => {
      const next: Record<string, BatchDraft> = {}
      for (const batch of batches) {
        next[batch.batch_key] = prev[batch.batch_key] ?? {
          order_url: batch.order_url ?? '',
          notes: batch.notes ?? '',
        }
      }
      return next
    })
  }, [batches])

  function patchDraft(key: string, patch: Partial<BatchDraft>) {
    setDrafts((prev) => ({ ...prev, [key]: { ...prev[key], ...patch } }))
  }

  function onSave(key: string) {
    const draft = drafts[key]
    if (!draft) return
    setFeedback({ message: '正在保存批次...' })
    updateBatch
      .mutateAsync({ batch_key: key, order_url: draft.order_url, notes: draft.notes })
      .then(() => setFeedback({ message: '批次已保存' }))
      .catch((e: Error) => setFeedback({ error: e.message }))
  }

  function onView(key: string) {
    setBatch(key)
    navigate({ to: '/' })
  }

  return (
    <main className="bg-background text-foreground min-h-screen p-6 md:p-10">
      <div className="mb-6 flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="text-[28px] font-extrabold tracking-tight">批次台账</h1>
          <p className="text-muted-foreground mt-1 text-sm">
            按采购批次聚合的账本。点击「查看」跳转到数据记录页并按批次筛选。
          </p>
        </div>
        <Button type="button" variant="outline" onClick={() => void batchesQuery.refetch()}>刷新账本</Button>
      </div>

      <div className="rounded-2xl border p-5 shadow-sm">
        <div className="mb-3 flex items-center gap-3">
          <span className="text-muted-foreground ml-auto text-[13px]">共 {batchesQuery.data?.total ?? 0} 个批次</span>
        </div>

        {(feedback.error || feedback.message) && (
          <div className={`mb-3 min-h-5 text-[13px] ${feedback.error ? 'text-destructive' : 'text-muted-foreground'}`}>
            {feedback.error ?? feedback.message}
          </div>
        )}

        {batchesQuery.isLoading ? (
          <div className="text-muted-foreground p-8 text-center text-sm">加载中...</div>
        ) : batches.length === 0 ? (
          <div className="text-muted-foreground p-8 text-center">暂无批次数据</div>
        ) : (
          <div className="overflow-auto rounded-xl border">
            <Table className="min-w-[1100px]">
              <TableHeader>
                <TableRow>
                  <TableHead>批次</TableHead>
                  <TableHead>采购日期</TableHead>
                  <TableHead>订单号</TableHead>
                  <TableHead>件数</TableHead>
                  <TableHead>总价</TableHead>
                  <TableHead>记录数</TableHead>
                  <TableHead>健康分布</TableHead>
                  <TableHead>授权分布</TableHead>
                  <TableHead>订单链接</TableHead>
                  <TableHead>备注</TableHead>
                  <TableHead>操作</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {batches.map((batch) => {
                  const health = healthText(batch.health ?? {})
                  const draft = drafts[batch.batch_key]
                  return (
                    <TableRow key={batch.batch_key}>
                      <TableCell>
                        <div>{shortBatchLabel(batch.batch_key)}</div>
                        <div className="text-muted-foreground font-mono text-[11px]">{batch.batch_key}</div>
                      </TableCell>
                      <TableCell>{batch.batch_date ?? '-'}</TableCell>
                      <TableCell>{batch.order_id ?? '-'}</TableCell>
                      <TableCell>{batch.quantity ?? '-'}</TableCell>
                      <TableCell>{batch.total_cost ? `${batch.total_cost} 元` : '-'}</TableCell>
                      <TableCell className="font-bold">{batch.record_count ?? 0}</TableCell>
                      <TableCell>
                        <span className={health.bad ? 'text-destructive font-bold' : 'font-bold text-green-600'}>
                          {health.text}
                        </span>
                      </TableCell>
                      <TableCell>{authText(batch.auth_state ?? {})}</TableCell>
                      <TableCell>
                        <Input
                          className="min-w-[140px] text-xs"
                          placeholder="订单查询链接"
                          value={draft?.order_url ?? ''}
                          onChange={(e) => patchDraft(batch.batch_key, { order_url: e.target.value })}
                        />
                      </TableCell>
                      <TableCell>
                        <Input
                          className="min-w-[140px] text-xs"
                          placeholder="备注（如：已售出）"
                          value={draft?.notes ?? ''}
                          onChange={(e) => patchDraft(batch.batch_key, { notes: e.target.value })}
                        />
                      </TableCell>
                      <TableCell>
                        <div className="flex gap-2">
                          <Button type="button" size="sm" onClick={() => onSave(batch.batch_key)}>保存</Button>
                          <Button type="button" size="sm" variant="outline" onClick={() => onView(batch.batch_key)}>查看</Button>
                        </div>
                      </TableCell>
                    </TableRow>
                  )
                })}
              </TableBody>
            </Table>
          </div>
        )}
      </div>
    </main>
  )
}

export const Route = createFileRoute('/batches')({
  component: BatchesPage,
})
