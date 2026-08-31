import { createRootRouteWithContext, Outlet, redirect } from '@tanstack/react-router'
import type { QueryClient } from '@tanstack/react-query'

import { hasManagementKey } from '@/lib/auth'

export interface RouterContext {
  queryClient: QueryClient
}

/** 根路由：未登录（无管理密钥）一律弹回 /login */
export const Route = createRootRouteWithContext<RouterContext>()({
  beforeLoad: ({ location }) => {
    if (!hasManagementKey() && location.pathname !== '/login') {
      throw redirect({ to: '/login' })
    }
  },
  component: () => <Outlet />,
})
