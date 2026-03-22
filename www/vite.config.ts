import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// https://vite.dev/config/
export default defineConfig({
  plugins: [react()],
  build: {
    // 使用 esbuild 激进压缩
    minify: 'esbuild',
    cssMinify: true,
    rollupOptions: {
      output: {
        // 入口 JS 文件
        entryFileNames: 'assets/js/[name]-[hash].js',
        // 代码分割的 chunk 文件
        chunkFileNames: 'assets/js/[name]-[hash].js',
        // 静态资源文件（CSS、字体、图片等）
        assetFileNames: (assetInfo) => {
          const name = assetInfo.name || ''
          // CSS 文件
          if (name.endsWith('.css')) {
            return 'assets/css/[name]-[hash][extname]'
          }
          // 字体文件
          if (/\.(woff2?|ttf|eot|otf)$/i.test(name)) {
            return 'assets/fonts/[name]-[hash][extname]'
          }
          // 图片文件
          if (/\.(png|jpe?g|gif|svg|webp|ico|bmp)$/i.test(name)) {
            return 'assets/img/[name]-[hash][extname]'
          }
          // 其他资源
          return 'assets/[name]-[hash][extname]'
        },
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
          // Recharts 图表库（较大，单独分离）
          if (id.includes('node_modules/recharts/')) {
            return 'vendor-recharts'
          }
          // Markdown 渲染库
          if (id.includes('node_modules/react-markdown/') ||
              id.includes('node_modules/remark-gfm/') ||
              id.includes('node_modules/unified/') ||
              id.includes('node_modules/remark-') ||
              id.includes('node_modules/mdast-') ||
              id.includes('node_modules/micromark/') ||
              id.includes('node_modules/unist-')) {
            return 'vendor-markdown'
          }
          // 其他第三方库
          if (id.includes('node_modules/axios/') ||
              id.includes('node_modules/opus-decoder/') ||
              id.includes('node_modules/react-easy-crop/') ||
              id.includes('node_modules/@minceraftmc/')) {
            return 'vendor'
          }
        },
      },
    },
  },
  server: {
    port: 9001, // 避免与 Windows NAT 保留端口冲突
    proxy: {
      '/api': {
        target: 'http://localhost:9002',
        changeOrigin: true,
      },
      '/ws': {
        target: 'http://localhost:9002',
        changeOrigin: true,
      },
    },
  },
})
