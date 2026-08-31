import { createFileRoute, useNavigate } from '@tanstack/react-router'
import { useState } from 'react'

import { setManagementKey } from '@/lib/auth'

/** 管理密钥登录页：密钥只存本机浏览器 localStorage */
function LoginPage() {
  const navigate = useNavigate()
  const [value, setValue] = useState('')

  function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    setManagementKey(value)
    if (value.trim()) navigate({ to: '/' })
  }

  return (
    <main className="bg-background flex min-h-screen items-center justify-center p-6">
      <form
        onSubmit={handleSubmit}
        className="bg-card text-card-foreground w-full max-w-sm space-y-4 rounded-xl border p-8 shadow-sm"
      >
        <div className="space-y-1.5">
          <h1 className="text-2xl font-bold tracking-tight">CPA 数据管理</h1>
          <p className="text-muted-foreground text-sm">输入 CLIProxyAPI 管理密钥以继续</p>
        </div>
        <input
          type="password"
          value={value}
          onChange={(e) => setValue(e.target.value)}
          placeholder="管理密钥（secret-key）"
          autoFocus
          className="border-input bg-background placeholder:text-muted-foreground focus:ring-ring h-10 w-full rounded-md border px-3 text-sm focus:outline-none focus:ring-2"
        />
        <button
          type="submit"
          disabled={!value.trim()}
          className="bg-primary text-primary-foreground hover:bg-primary/90 h-10 w-full rounded-md text-sm font-semibold disabled:opacity-50"
        >
          进入
        </button>
        <p className="text-muted-foreground text-xs">
          密钥仅保存在本机浏览器 localStorage，不会上传到任何服务器；密钥可在服务端 config.yaml 的
          secret-key 中找回。
        </p>
      </form>
    </main>
  )
}

export const Route = createFileRoute('/login')({
  component: LoginPage,
})
