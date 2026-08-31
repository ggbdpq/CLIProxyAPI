import { createRootRouteWithContext, Link, Outlet, redirect } from '@tanstack/react-router'
import type { QueryClient } from '@tanstack/react-query'

import { hasManagementKey } from '@/lib/auth'

export interface RouterContext {
  queryClient: QueryClient
}

const NAV_TABS = [
  { to: '/', label: '数据记录' },
  { to: '/batches', label: '批次台账' },
  { to: '/panel', label: '系统面板' },
] as const

function NavTab({ to, label }: { to: (typeof NAV_TABS)[number]['to']; label: string }) {
  return (
    <Link
      to={to}
      className="text-muted-foreground hover:text-foreground inline-flex h-12 items-center rounded-md px-3 text-sm font-medium transition-colors"
      activeProps={{
        className: 'text-foreground bg-muted font-bold',
      }}
    >
      {label}
    </Link>
  )
}

/** 根路由：未登录（无管理密钥）一律弹回 /login；登录后顶部共享导航条 */
export const Route = createRootRouteWithContext<RouterContext>()({
  beforeLoad: ({ location }) => {
    if (!hasManagementKey() && location.pathname !== '/login') {
      throw redirect({ to: '/login' })
    }
  },
  component: () => (
    <div className="bg-background text-foreground min-h-screen">
      <nav className="border-b bg-background/95 sticky top-0 z-20 flex h-12 items-center gap-1 px-4 backdrop-blur">
        <span className="mr-3 text-sm font-extrabold tracking-tight">数据管理</span>
        {NAV_TABS.map((tab) => (
          <NavTab key={tab.to} to={tab.to} label={tab.label} />
        ))}
      </nav>
      <Outlet />
    </div>
  ),
})
