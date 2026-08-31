import { Button } from '@/components/ui/button'
import { useDetection } from '@/store/app'

/** 配额检测进度面板：全局 store 驱动，翻页/切筛选时循环持续 */
export function DetectPanel() {
  const detect = useDetection()
  const running = detect.running

  const text = !running && detect.done === 0 && detect.total === 0
    ? '未在检测中'
    : `检测进度 ${detect.done}/${detect.total}，正常 ${detect.ok}，失败 ${detect.failed}，跳过 ${detect.skipped}${detect.note ? `，${detect.note}` : ''}`

  return (
    <div className="text-muted-foreground flex flex-col gap-1.5 text-sm font-bold sm:flex-row sm:items-center">
      <span className="flex-1 basis-full">{text}</span>
      <Button type="button" variant="destructive" size="sm" disabled={!running} onClick={detect.stop}>
        停止检测
      </Button>
    </div>
  )
}
