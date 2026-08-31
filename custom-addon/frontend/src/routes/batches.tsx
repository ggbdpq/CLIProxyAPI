import { createFileRoute } from '@tanstack/react-router'

/** 批次台账页（阶段 3 实装） */
function BatchesPage() {
  return (
    <main className="bg-background text-foreground min-h-screen p-10">
      <h1 className="text-2xl font-bold tracking-tight">批次台账</h1>
      <p className="text-muted-foreground mt-2 text-sm">阶段 3 实装：批量录入、保存、台账列表。</p>
    </main>
  )
}

export const Route = createFileRoute('/batches')({
  component: BatchesPage,
})
