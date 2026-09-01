import { createFileRoute, Link } from '@tanstack/react-router'
import { useRef, useState } from 'react'

import { StatsCards } from '@/components/records/stats-cards'
import { RecordsTable } from '@/components/records/records-table'
import { DetectPanel } from '@/components/records/detect-panel'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import {
  collectFilteredRecords,
  exportRecordsJSONL,
  useBatchesQuery,
  useDeleteRecordsMutation,
  useDeployRecordsMutation,
  useGenerateQuotaMutation,
  useImportMutation,
  useRecycleRecordsMutation,
  useRecordsQuery,
  useRefreshTokenMutation,
  useUpdateStateMutation,
} from '@/lib/queries'
import {
  HEALTH_LABELS,
  LIFECYCLE_LABELS,
  recordEmail,
  recordRefreshToken,
  type GenerateQuotaResult,
} from '@/lib/schemas'
import { useStatsQuery } from '@/lib/queries'
import { detectTargetsFrom, useDetection, useFilters } from '@/store/app'

const ALL = '__all__'
const DEPLOY_TARGET_LABELS: Record<string, string> = {
  local: '本仓库',
  official: '官方 auth 目录',
}

type FileActionResult = {
  title: string
  description: string
  files: string[]
}

function DataRecordsPage() {
  const filters = useFilters()
  const detection = useDetection()
  const fileInput = useRef<HTMLInputElement>(null)
  const [lifecycleValue, setLifecycleValue] = useState('unused')
  const [deployTarget, setDeployTarget] = useState('local')
  const [quotaResult, setQuotaResult] = useState<GenerateQuotaResult | null>(null)
  const [fileActionResult, setFileActionResult] = useState<FileActionResult | null>(null)
  const [refreshing, setRefreshing] = useState(false)
  const [feedback, setFeedback] = useState<{ error?: string; message?: string }>({})

  const recordsQuery = useRecordsQuery()
  const batchesQuery = useBatchesQuery()
  const deleteMutation = useDeleteRecordsMutation()
  const updateStateMutation = useUpdateStateMutation()
  const generateQuotaMutation = useGenerateQuotaMutation()
  const deployMutation = useDeployRecordsMutation()
  const recycleMutation = useRecycleRecordsMutation()
  const refreshTokenMutation = useRefreshTokenMutation()
  const importMutation = useImportMutation()

  const records = recordsQuery.data?.records ?? []
  const total = recordsQuery.data?.total ?? 0
  const pageCount = Math.max(1, Math.ceil(total / filters.pageSize))
  const selectedIds = filters.selectedIds()
  const { data: stats } = useStatsQuery()
  const authStateKeys = Object.keys(stats?.auth_state ?? {})

  function run<T>(fn: () => Promise<T>, busy: string) {
    setFeedback({ message: busy })
    fn().catch((e: Error) => setFeedback({ error: e.message, message: undefined }))
  }

  function onDeleteOne(id: number) {
    if (!window.confirm(`确定删除记录 ${id} 吗？`)) return
    run(async () => {
      const { deleted } = await deleteMutation.mutateAsync({ ids: [id] })
      setFeedback({ message: `已删除 ${deleted} 条数据` })
    }, '正在删除...')
  }

  function onDeleteSelected() {
    if (!selectedIds.length) return setFeedback({ error: '请选择要删除的数据' })
    if (!window.confirm(`确定删除选中的 ${selectedIds.length} 条数据吗？`)) return
    run(async () => {
      const { deleted } = await deleteMutation.mutateAsync({ ids: selectedIds })
      setFeedback({ message: `已删除 ${deleted} 条数据` })
    }, '正在删除选中数据...')
  }

  function onDeleteAll() {
    if (!total) return setFeedback({ error: '暂无数据可删除' })
    if (!window.confirm(`确定删除全部 ${total} 条数据吗？此操作不可恢复。`)) return
    run(async () => {
      const { deleted } = await deleteMutation.mutateAsync({ all: true })
      setFeedback({ message: `已删除 ${deleted} 条数据` })
    }, '正在删除全部数据...')
  }

  function onApplyLifecycle() {
    if (!selectedIds.length) return setFeedback({ error: '请先勾选要修改的数据' })
    run(async () => {
      const { updated } = await updateStateMutation.mutateAsync({ ids: selectedIds, lifecycle: lifecycleValue })
      setFeedback({ message: `已更新 ${updated} 条业务状态为「${LIFECYCLE_LABELS[lifecycleValue as keyof typeof LIFECYCLE_LABELS] ?? lifecycleValue}」` })
    }, '正在更新业务状态...')
  }

  function onGenerateQuota() {
    if (!selectedIds.length) return setFeedback({ error: '请选择要兼容生成的数据' })
    run(async () => {
      const result = await generateQuotaMutation.mutateAsync(selectedIds)
      setFeedback({ message: `已兼容生成 ${result.exported} 条本仓库凭证文件` })
      setQuotaResult(result)
    }, '正在兼容生成本仓库凭证文件...')
  }

  function onDeploy() {
    if (!selectedIds.length) return setFeedback({ error: '请选择要部署的数据' })
    run(async () => {
      const result = await deployMutation.mutateAsync({ ids: selectedIds, target: deployTarget })
      const label = DEPLOY_TARGET_LABELS[deployTarget] ?? deployTarget
      setFeedback({ message: `已部署 ${result.deployed} 条账号到「${label}」` })
      setFileActionResult({
        title: '账号已部署',
        description: `已部署 ${result.deployed} 条账号到 ${result.output_dir}。`,
        files: result.files,
      })
    }, '正在部署账号...')
  }

  function onRecycle() {
    if (!selectedIds.length) return setFeedback({ error: '请选择要回收的数据' })
    const label = DEPLOY_TARGET_LABELS[deployTarget] ?? deployTarget
    if (!window.confirm(`确定从「${label}」回收选中的 ${selectedIds.length} 条账号吗？`)) return
    run(async () => {
      const result = await recycleMutation.mutateAsync({ ids: selectedIds, target: deployTarget })
      setFeedback({ message: `已回收 ${result.recycled} 个凭证文件` })
      setFileActionResult({
        title: '账号已回收',
        description: `已从 ${result.output_dir} 回收 ${result.recycled} 个凭证文件。`,
        files: result.files,
      })
    }, '正在回收账号...')
  }

  async function onRefreshTokens() {
    if (refreshing) return
    const pool = records.filter((record) => selectedIds.includes(record.id) && recordRefreshToken(record.data))
    if (!pool.length) return setFeedback({ error: '请勾选要刷新令牌的记录（需要 refresh_token）' })
    setRefreshing(true)
    let ok = 0
    let failed = 0
    for (let i = 0; i < pool.length; i++) {
      const record = pool[i]
      setFeedback({ message: `正在刷新令牌 ${i + 1}/${pool.length}：${recordEmail(record.data) || `#${record.id}`}` })
      try {
        const result = await refreshTokenMutation.mutateAsync(record.id)
        if (result.ok) ok += 1
        else failed += 1
      } catch {
        failed += 1
      }
    }
    setRefreshing(false)
    setFeedback({ message: `令牌刷新结束：成功 ${ok}，失败 ${failed}；失败记录如为 invalid_grant 已标记需重授权` })
    void recordsQuery.refetch()
  }

  function onImport(file: File | undefined) {
    if (!file) return setFeedback({ error: '请选择 .jsonl 文件' })
    run(async () => {
      const { imported, stats } = await importMutation.mutateAsync(file)
      const parts = [`导入成功：新增/更新 ${imported} 条`]
      if (stats?.skipped_existing) parts.push(`跳过重复 ${stats.skipped_existing} 条`)
      if (stats?.enriched_existing) parts.push(`回填 ${stats.enriched_existing} 条`)
      setFeedback({ message: parts.join('，') })
    }, '正在导入并写入 SQLite...')
  }

  function onExport() {
    if (!selectedIds.length) return setFeedback({ error: '请选择要导出的数据' })
    run(async () => {
      await exportRecordsJSONL(selectedIds)
      setFeedback({ message: `已导出 ${selectedIds.length} 条数据` })
    }, '正在导出 JSONL...')
  }

  function filterQueryString(): string {
    const parts: string[] = []
    if (filters.q) parts.push(`q=${encodeURIComponent(filters.q)}`)
    if (filters.lifecycle) parts.push(`lifecycle=${encodeURIComponent(filters.lifecycle)}`)
    if (filters.health) parts.push(`health=${encodeURIComponent(filters.health)}`)
    if (filters.authState) parts.push(`auth_state=${encodeURIComponent(filters.authState)}`)
    if (filters.batch) parts.push(`batch=${encodeURIComponent(filters.batch)}`)
    if (filters.detected) parts.push('detected=1')
    return parts.length ? `&${parts.join('&')}` : ''
  }

  function onStartDetection(filteredOnly: boolean) {
    if (detection.running) return
    run(async () => {
      const pool = filteredOnly ? await collectFilteredRecords(filterQueryString()) : records.filter((r) => selectedIds.includes(r.id))
      const targets = detectTargetsFrom(pool)
      if (!targets.length) return setFeedback({ error: '没有可检测的记录（需要 email 和 access_token）' })
      setFeedback({ message: '检测已开始...' })
      void detection.start(targets)
    }, '正在收集检测目标...')
  }

  const batches = batchesQuery.data?.batches ?? []

  return (
    <main className="bg-background text-foreground min-h-screen p-6 md:p-10">
      <div className="mb-6 flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="text-[28px] font-extrabold tracking-tight">数据记录</h1>
          <p className="text-muted-foreground mt-1 text-sm">导入 JSONL 到本地 SQLite，集中管理与检测账号数据。</p>
        </div>
      </div>

      <div className="rounded-2xl border p-5 shadow-sm">
        {/* 统计卡片 */}
        <StatsCards />

        {/* 检测面板 */}
        <div className="mb-4">
          <DetectPanel />
        </div>

        {/* 工具栏 */}
        <div className="mb-4 flex flex-wrap items-center gap-3">
          <input
            ref={fileInput}
            type="file"
            accept=".jsonl"
            className="hidden"
            onChange={(e) => {
              onImport(e.target.files?.[0])
              e.target.value = ''
            }}
          />
          <Button type="button" variant="outline" onClick={() => fileInput.current?.click()}>导入 JSONL</Button>
          <Button type="button" variant="outline" asChild>
            <Link to="/tools">登记重授权</Link>
          </Button>
          <Button type="button" variant="outline" onClick={onExport} disabled={!selectedIds.length}>导出选中</Button>
          <Button type="button" variant="outline" onClick={() => { filters.resetPage(); void recordsQuery.refetch() }}>刷新</Button>
          <Button type="button" variant="outline" onClick={() => onStartDetection(false)} disabled={!selectedIds.length}>检测选中</Button>
          <Button type="button" variant="outline" onClick={() => onStartDetection(true)}>检测筛选结果</Button>
          <span className="text-muted-foreground ml-auto text-[13px]">共 {total} 条</span>
        </div>

        {/* 筛选条 */}
        <div className="mb-4 flex flex-wrap items-center gap-2">
          <Input
            className="w-[220px] rounded-full"
            placeholder="搜索..."
            defaultValue={filters.q}
            onKeyDown={(e) => { if (e.key === 'Enter') filters.setQ(e.currentTarget.value.trim()) }}
          />
          <Select value={filters.lifecycle || ALL} onValueChange={(v) => filters.setLifecycle(v === ALL ? '' : v)}>
            <SelectTrigger className="w-[130px] rounded-full"><SelectValue placeholder="生命周期" /></SelectTrigger>
            <SelectContent>
              <SelectItem value={ALL}>全部状态</SelectItem>
              {Object.entries(LIFECYCLE_LABELS).map(([value, label]) => (
                <SelectItem key={value} value={value}>{label}</SelectItem>
              ))}
            </SelectContent>
          </Select>
          <Select value={filters.health || ALL} onValueChange={(v) => filters.setHealth(v === ALL ? '' : v)}>
            <SelectTrigger className="w-[130px] rounded-full"><SelectValue placeholder="健康" /></SelectTrigger>
            <SelectContent>
              <SelectItem value={ALL}>全部健康</SelectItem>
              {Object.entries(HEALTH_LABELS).map(([value, label]) => (
                <SelectItem key={value} value={value}>{label}</SelectItem>
              ))}
            </SelectContent>
          </Select>
          <Select value={filters.authState || ALL} onValueChange={(v) => filters.setAuthState(v === ALL ? '' : v)}>
            <SelectTrigger className="w-[140px] rounded-full"><SelectValue placeholder="授权状态" /></SelectTrigger>
            <SelectContent>
              <SelectItem value={ALL}>全部授权</SelectItem>
              {authStateKeys.map((value) => (
                <SelectItem key={value} value={value}>
                  {value === 'authorized' ? '已授权' : value}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <Select value={filters.batch || ALL} onValueChange={(v) => filters.setBatch(v === ALL ? '' : v)}>
            <SelectTrigger className="w-[160px] rounded-full"><SelectValue placeholder="批次" /></SelectTrigger>
            <SelectContent>
              <SelectItem value={ALL}>全部批次</SelectItem>
              {batches.map((batch) => (
                <SelectItem key={batch.batch_key} value={batch.batch_key}>{batch.batch_key}</SelectItem>
              ))}
            </SelectContent>
          </Select>
          <Button
            type="button"
            variant="ghost"
            size="sm"
            onClick={() => { filters.setLifecycle(''); filters.setHealth(''); filters.setAuthState(''); filters.setBatch(''); filters.setDetected(false) }}
          >
            清空筛选
          </Button>
        </div>

        {/* 批量操作 */}
        <div className="mb-4 flex flex-wrap items-center gap-3">
          <div className="flex items-center gap-2">
            <Select value={lifecycleValue} onValueChange={setLifecycleValue}>
              <SelectTrigger className="h-8 w-[120px]"><SelectValue /></SelectTrigger>
              <SelectContent>
                {Object.entries(LIFECYCLE_LABELS).map(([value, label]) => (
                  <SelectItem key={value} value={value}>{label}</SelectItem>
                ))}
              </SelectContent>
            </Select>
            <Button type="button" variant="outline" size="sm" onClick={onApplyLifecycle} disabled={!selectedIds.length}>
              应用状态
            </Button>
          </div>
          <Button type="button" variant="outline" size="sm" onClick={onGenerateQuota} disabled={!selectedIds.length}>兼容生成</Button>
          <div className="flex items-center gap-2">
            <Select value={deployTarget} onValueChange={setDeployTarget}>
              <SelectTrigger className="h-8 w-[150px]"><SelectValue /></SelectTrigger>
              <SelectContent>
                {Object.entries(DEPLOY_TARGET_LABELS).map(([value, label]) => (
                  <SelectItem key={value} value={value}>{label}</SelectItem>
                ))}
              </SelectContent>
            </Select>
            <Button type="button" variant="outline" size="sm" onClick={onDeploy} disabled={!selectedIds.length}>部署选中</Button>
            <Button type="button" variant="outline" size="sm" onClick={onRecycle} disabled={!selectedIds.length}>回收选中</Button>
          </div>
          <Button type="button" variant="outline" size="sm" onClick={() => void onRefreshTokens()} disabled={!selectedIds.length || refreshing}>
            {refreshing ? '刷新中...' : '刷新令牌'}
          </Button>
          <Button type="button" variant="destructive" size="sm" onClick={onDeleteSelected} disabled={!selectedIds.length}>
            删除选中（{selectedIds.length}）
          </Button>
          <Button type="button" variant="destructive" size="sm" onClick={onDeleteAll} disabled={!total}>删除全部</Button>
        </div>

        {/* 消息行 */}
        {(feedback.error || feedback.message || recordsQuery.error) && (
          <div className={`mb-3 min-h-5 text-[13px] ${feedback.error || recordsQuery.error ? 'text-destructive' : 'text-muted-foreground'}`}>
            {feedback.error ?? recordsQuery.error?.message ?? feedback.message}
          </div>
        )}

        {/* 表格 */}
        {recordsQuery.isLoading ? (
          <div className="text-muted-foreground p-8 text-center text-sm">正在读取 SQLite 数据...</div>
        ) : (
          <RecordsTable records={records} onDeleteOne={onDeleteOne} />
        )}

        {/* 分页 */}
        <div className="mt-4 flex items-center justify-end gap-1.5">
          <Button type="button" variant="outline" size="sm" disabled={filters.page <= 1 || recordsQuery.isLoading} onClick={() => filters.goToPage(-1)}>上一页</Button>
          <span className="text-muted-foreground px-2 text-[13px] font-bold">
            {filters.page} / {pageCount}
          </span>
          <Button type="button" variant="outline" size="sm" disabled={filters.page >= pageCount || recordsQuery.isLoading} onClick={() => filters.goToPage(1)}>下一页</Button>
        </div>
      </div>

      {/* 配额生成结果 */}
      <Dialog open={quotaResult !== null} onOpenChange={(open) => !open && setQuotaResult(null)}>
        <DialogContent className="max-w-[520px]">
          <DialogHeader>
            <DialogTitle>本仓库凭证文件已生成</DialogTitle>
            <DialogDescription>
              已兼容生成 {quotaResult?.exported ?? 0} 条凭证文件{quotaResult?.output_dir ? `到 ${quotaResult.output_dir}` : ''}。
            </DialogDescription>
          </DialogHeader>
          {quotaResult?.files?.length ? (
            <ul className="max-h-[240px] list-disc overflow-auto pl-4 text-sm">
              {quotaResult.files.map((name) => <li key={name} className="font-mono text-xs">{name}</li>)}
            </ul>
          ) : null}
        </DialogContent>
      </Dialog>

      {/* 部署 / 回收结果 */}
      <Dialog open={fileActionResult !== null} onOpenChange={(open) => !open && setFileActionResult(null)}>
        <DialogContent className="max-w-[520px]">
          <DialogHeader>
            <DialogTitle>{fileActionResult?.title}</DialogTitle>
            <DialogDescription>{fileActionResult?.description}</DialogDescription>
          </DialogHeader>
          {fileActionResult?.files.length ? (
            <ul className="max-h-[240px] list-disc overflow-auto pl-4 text-sm">
              {fileActionResult.files.map((name) => <li key={name} className="font-mono text-xs">{name}</li>)}
            </ul>
          ) : null}
        </DialogContent>
      </Dialog>
    </main>
  )
}

export const Route = createFileRoute('/')({
  component: DataRecordsPage,
})
