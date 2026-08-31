import { createFileRoute } from '@tanstack/react-router'

/** 系统面板：同源 iframe 内嵌官方面板（#/system 配额管理等），与数据管理同窗操作。
 *  官方面板自行管理登录（同一管理密钥），配额刷新经后端自动回写数据管理库。 */
function PanelPage() {
  return (
    <main className="bg-background text-foreground h-[calc(100dvh-3rem)]">
      <iframe
        src="/management.html#/system"
        title="CLIProxyAPI 系统面板"
        className="h-full w-full border-0"
      />
    </main>
  )
}

export const Route = createFileRoute('/panel')({
  component: PanelPage,
})
