import viteReact from '@vitejs/plugin-react'
import { defineConfig } from 'vitest/config'

// Separate from vite.config.ts on purpose: unit tests don't need (or want)
// the TanStack Start plugin, which spins up SSR/router codegen machinery.
export default defineConfig({
  plugins: [viteReact()],
  test: {
    environment: 'jsdom',
    globals: true,
  },
})
