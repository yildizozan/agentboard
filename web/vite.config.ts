import { defineConfig } from 'vite'

// The Go binary embeds dist/; during `npm run dev` API calls go to a running `agentboard board`.
export default defineConfig({
  build: {
    outDir: 'dist',
    emptyOutDir: true,
  },
  server: {
    proxy: {
      '/api': 'http://127.0.0.1:7420',
    },
  },
})
