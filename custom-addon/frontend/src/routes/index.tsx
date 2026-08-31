import { createFileRoute } from '@tanstack/react-router'

/** 数据记录主页：列表 / 检索筛选 / 批量 / 配额检测（阶段 3 实装） */
function DataRecordsPage() {
  return (
    <main className="bg-background text-foreground min-h-screen p-10">
      <h1 className="text-2xl font-bold tracking-tight">数据记录</h1>
      <p className="text-muted-foreground mt-2 text-sm">
        阶段 3 实装：记录 CRUD、检索筛选、批量操作、配额检测面板。
      </p>
    </main>
  )
}

export const Route = createFileRoute('/')({
  component: DataRecordsPage,
})
