import { fileURLToPath } from 'node:url'
import tailwindcss from '@tailwindcss/vite'
import { tanstackRouter } from '@tanstack/router-plugin/vite'
import react from '@vitejs/plugin-react'
import { defineConfig } from 'vite'

// SPA 挂载于 Go 后端的 /data-mgmt/ 子路径，base 必须与后端路由前缀一致。
export default defineConfig({
  base: '/data-mgmt/',
  plugins: [
    // 路由代码生成需在 react 插件之前
    tanstackRouter({ target: 'react', autoCodeSplitting: true }),
    react(),
    tailwindcss(),
  ],
  resolve: {
    alias: { '@': fileURLToPath(new URL('./src', import.meta.url)) },
  },
  server: {
    port: 5173,
    // 开发模式下代理到本机 CLIProxyAPI 实例（8318）；/management.html 供 /panel 页 iframe 使用
    proxy: {
      '/v0': 'http://127.0.0.1:8318',
      '/management.html': 'http://127.0.0.1:8318',
    },
  },
})
