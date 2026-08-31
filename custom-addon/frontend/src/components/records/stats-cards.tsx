import { cn } from '@/lib/utils'
import { useStatsQuery } from '@/lib/queries'
import { useFilters } from '@/store/app'

interface StatCard {
  filter: string
  num: number
  label: string
}

/** 统计卡片：点击联动筛选（与旧版一致——lifecycle/异常切换、总量清空全部筛选） */
export function StatsCards() {
  const { data: stats, abnormal } = useStatsQuery()
  const filters = useFilters()
  const lifecycle = stats?.lifecycle ?? {}

  const cards: StatCard[] = [
    { filter: '', num: stats?.total ?? 0, label: '总量' },
    { filter: 'lifecycle:unused', num: lifecycle.unused ?? 0, label: '未用' },
    { filter: 'lifecycle:in_use', num: lifecycle.in_use ?? 0, label: '使用中' },
    { filter: 'lifecycle:sold', num: lifecycle.sold ?? 0, label: '已售出' },
    { filter: 'health:abnormal', num: abnormal, label: '异常' },
  ]

  function active(filter: string): boolean {
    if (filter === 'lifecycle:unused') return filters.lifecycle === 'unused'
    if (filter === 'lifecycle:in_use') return filters.lifecycle === 'in_use'
    if (filter === 'lifecycle:sold') return filters.lifecycle === 'sold'
    if (filter === 'health:abnormal') return filters.health === 'abnormal'
    return filters.lifecycle === '' && filters.health === '' && filters.authState === '' && filters.batch === ''
  }

  function apply(filter: string) {
    if (filter.startsWith('lifecycle:')) {
      const value = filter.split(':')[1]
      filters.setLifecycle(filters.lifecycle === value ? '' : value)
      filters.setHealth('')
    } else if (filter === 'health:abnormal') {
      filters.setHealth(filters.health === 'abnormal' ? '' : 'abnormal')
      filters.setLifecycle('')
    } else {
      filters.setLifecycle('')
      filters.setHealth('')
      filters.setAuthState('')
      filters.setBatch('')
    }
  }

  return (
    <div className="mb-4 flex flex-wrap gap-2.5">
      {cards.map((card) => (
        <button
          key={card.label}
          type="button"
          onClick={() => apply(card.filter)}
          className={cn(
            'flex min-w-[120px] flex-1 flex-col gap-0.5 rounded-xl border bg-secondary px-4 py-2.5 text-left',
            active(card.filter) && 'border-primary shadow-[0_0_0_2px_rgba(17,24,39,.08)]',
          )}
        >
          <span className="text-[22px] font-extrabold">{card.num}</span>
          <span className="text-muted-foreground text-xs font-bold">{card.label}</span>
        </button>
      ))}
    </div>
  )
}
