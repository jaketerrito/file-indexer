import viteReact from '@vitejs/plugin-react'
import { defineConfig } from 'vitest/config'

// Separate from vite.config.ts on purpose: unit tests don't need (or want)
// the TanStack Start plugin, which spins up SSR/router codegen machinery.
export default defineConfig({
  plugins: [viteReact()],
  test: {
    environment: 'jsdom',
    globals: true,
    coverage: {
      provider: 'v8',
      // Mirrors .testcoverage.yml for Go: exclude generated code and thin
      // composition/wiring files (the web analog of cmd/ main.go files);
      // everything with real logic must stay covered.
      include: ['src/**'],
      exclude: [
        'src/gen/**', // generated protobuf
        'src/routeTree.gen.ts', // generated route tree
        'src/router.tsx', // router wiring
        'src/routes/**', // route shells; logic lives in lib/ and components/
        'src/server/clients.ts', // gRPC transport construction
        'src/server/files.ts', // thin serverFn wrappers around impl.ts
      ],
      thresholds: {
        statements: 80,
        branches: 80,
        functions: 80,
        lines: 80,
      },
    },
  },
})
