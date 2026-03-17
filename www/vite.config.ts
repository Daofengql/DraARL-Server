import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// https://vite.dev/config/
export default defineConfig({
  plugins: [react()],
  build: {
    rollupOptions: {
      output: {
        manualChunks(id) {
          // React 核心库和 emotion（emotion 依赖 react，必须放在一起）
          if (id.includes('node_modules/react/') ||
              id.includes('node_modules/react-dom/') ||
              id.includes('node_modules/react-router-dom/') ||
              id.includes('node_modules/@emotion/') ||
              id.includes('node_modules/scheduler/')) {
            return 'vendor-react'
          }
          // MUI 组件库
          if (id.includes('node_modules/@mui/')) {
            return 'vendor-mui'
          }
          // 其他第三方库
          if (id.includes('node_modules/axios/') ||
              id.includes('node_modules/opus-decoder/') ||
              id.includes('node_modules/react-easy-crop/')) {
            return 'vendor'
          }
        },
      },
    },
  },
  server: {
    proxy: {
      '/api': {
        target: 'http://localhost:9000',
        changeOrigin: true,
      },
    },
  },
})
