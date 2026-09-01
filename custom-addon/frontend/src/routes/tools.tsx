import { createFileRoute } from '@tanstack/react-router'
import { useRef, useState } from 'react'

import { Button } from '@/components/ui/button'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { convertDataRecords, useRegisterReauthMutation } from '@/lib/queries'
import type { ConvertResult, RegisterReauthResult } from '@/lib/schemas'

const FORMATS = [
  { value: 'txt', label: '卡密 TXT' },
  { value: 'cpa', label: 'CPA JSONL' },
  { value: 'sub2api', label: 'sub2api JSON' },
] as const

const SUPPORTED = new Set(['txt:cpa', 'cpa:sub2api', 'sub2api:cpa'])

function downloadText(filename: string, content: string) {
  const blob = new Blob([content], { type: 'text/plain;charset=utf-8' })
  const url = URL.createObjectURL(blob)
  const anchor = document.createElement('a')
  anchor.href = url
  anchor.download = filename
  document.body.appendChild(anchor)
  anchor.click()
  anchor.remove()
  URL.revokeObjectURL(url)
}

function resultLine(result: RegisterReauthResult): string {
  return `总计 ${result.total}，更新 ${result.updated}，新增 ${result.imported}，未匹配 ${result.unmatched}，缺邮箱 ${result.missing_email}，缺 token ${result.missing_token}，跳过 ${result.skipped}`
}

function ToolsPage() {
  const inputFile = useRef<HTMLInputElement>(null)
  const reauthFile = useRef<HTMLInputElement>(null)
  const [from, setFrom] = useState('txt')
  const [to, setTo] = useState('cpa')
  const [input, setInput] = useState('')
  const [output, setOutput] = useState<ConvertResult | null>(null)
  const [importUnmatched, setImportUnmatched] = useState(true)
  const [feedback, setFeedback] = useState<{ error?: string; message?: string }>({})
  const reauthMutation = useRegisterReauthMutation()
  const canConvert = SUPPORTED.has(`${from}:${to}`) && input.trim() !== ''

  async function loadInputFile(file: File | undefined) {
    if (!file) return
    setInput(await file.text())
    setFeedback({ message: `已读取 ${file.name}` })
  }

  async function onConvert() {
    if (!canConvert) return setFeedback({ error: '只支持 TXT→CPA、CPA→sub2api、sub2api→CPA，且输入不能为空' })
    setFeedback({ message: '正在转换...' })
    try {
      const result = await convertDataRecords({ from, to, content: input })
      setOutput(result)
      setFeedback({ message: `转换完成：${result.converted} 条` })
    } catch (e) {
      setFeedback({ error: e instanceof Error ? e.message : '转换失败' })
    }
  }

  async function onRegisterReauth(file: File | undefined) {
    if (!file) return
    setFeedback({ message: '正在登记重授权结果...' })
    try {
      const result = await reauthMutation.mutateAsync({ file, importUnmatched })
      setFeedback({ message: `重授权登记完成：${resultLine(result)}` })
    } catch (e) {
      setFeedback({ error: e instanceof Error ? e.message : '重授权登记失败' })
    }
  }

  return (
    <main className="bg-background text-foreground min-h-screen p-6 md:p-10">
      <div className="mb-6">
        <h1 className="text-[28px] font-extrabold tracking-tight">工具</h1>
        <p className="text-muted-foreground mt-1 text-sm">格式转换只处理粘贴或上传内容；重授权登记才会写入库存。</p>
      </div>

      <section className="mb-6 rounded-2xl border p-5 shadow-sm">
        <div className="mb-4 flex flex-wrap items-center gap-3">
          <h2 className="mr-auto text-lg font-bold">格式转换</h2>
          <Select value={from} onValueChange={setFrom}>
            <SelectTrigger className="w-[150px]"><SelectValue /></SelectTrigger>
            <SelectContent>
              {FORMATS.map((item) => <SelectItem key={item.value} value={item.value}>{item.label}</SelectItem>)}
            </SelectContent>
          </Select>
          <span className="text-muted-foreground text-sm">→</span>
          <Select value={to} onValueChange={setTo}>
            <SelectTrigger className="w-[150px]"><SelectValue /></SelectTrigger>
            <SelectContent>
              {FORMATS.map((item) => <SelectItem key={item.value} value={item.value}>{item.label}</SelectItem>)}
            </SelectContent>
          </Select>
          <input
            ref={inputFile}
            type="file"
            accept=".txt,.json,.jsonl"
            className="hidden"
            onChange={(e) => {
              void loadInputFile(e.target.files?.[0])
              e.target.value = ''
            }}
          />
          <Button type="button" variant="outline" onClick={() => inputFile.current?.click()}>上传输入</Button>
          <Button type="button" onClick={() => void onConvert()} disabled={!canConvert}>转换</Button>
          <Button
            type="button"
            variant="outline"
            disabled={!output}
            onClick={() => output && downloadText(output.filename, output.content)}
          >
            下载结果
          </Button>
        </div>
        <div className="grid gap-4 lg:grid-cols-2">
          <textarea
            className="border-input bg-background min-h-[360px] rounded-xl border p-3 font-mono text-xs outline-none"
            placeholder="粘贴卡密 TXT / CPA JSONL / sub2api JSON"
            value={input}
            onChange={(e) => setInput(e.target.value)}
          />
          <textarea
            className="border-input bg-muted/30 min-h-[360px] rounded-xl border p-3 font-mono text-xs outline-none"
            placeholder="转换结果"
            value={output?.content ?? ''}
            onChange={(e) => setOutput(output ? { ...output, content: e.target.value } : null)}
          />
        </div>
      </section>

      <section className="rounded-2xl border p-5 shadow-sm">
        <div className="mb-3 flex flex-wrap items-center gap-3">
          <h2 className="mr-auto text-lg font-bold">重授权登记</h2>
          <label className="text-muted-foreground flex items-center gap-2 text-sm">
            <input
              type="checkbox"
              checked={importUnmatched}
              onChange={(e) => setImportUnmatched(e.target.checked)}
            />
            未匹配邮箱作为新记录入库
          </label>
          <input
            ref={reauthFile}
            type="file"
            accept=".txt,.jsonl"
            className="hidden"
            onChange={(e) => {
              void onRegisterReauth(e.target.files?.[0])
              e.target.value = ''
            }}
          />
          <Button type="button" onClick={() => reauthFile.current?.click()} disabled={reauthMutation.isPending}>
            {reauthMutation.isPending ? '登记中...' : '上传重授权产物'}
          </Button>
        </div>
        <p className="text-muted-foreground text-sm">
          按邮箱匹配库存并回写新 token、重置后过期日期和本次重授权日期；成功后清掉旧 quota/nextTime，健康状态回到未测。
        </p>
      </section>

      {(feedback.error || feedback.message) && (
        <div className={`mt-4 text-sm ${feedback.error ? 'text-destructive' : 'text-muted-foreground'}`}>
          {feedback.error ?? feedback.message}
        </div>
      )}
    </main>
  )
}

export const Route = createFileRoute('/tools')({
  component: ToolsPage,
})
