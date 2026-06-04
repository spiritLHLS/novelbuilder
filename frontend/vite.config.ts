import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import { resolve } from 'path'

export default defineConfig({
  plugins: [vue()],
  resolve: {
    alias: {
      '@': resolve(__dirname, 'src'),
    },
  },
  server: {
    port: 3000,
    proxy: {
      '/api': {
        target: 'http://localhost:8080',
        changeOrigin: true,
      },
    },
  },
  build: {
    outDir: 'dist',
    sourcemap: false,
    rolldownOptions: {
      output: {
        codeSplitting: {
          groups: [
            {
              name: 'vendor-vue',
              test: /node_modules[\\/](vue|@vue|vue-router|pinia|@vueuse)[\\/]/,
              priority: 40,
            },
            {
              name: 'vendor-element',
              test: /node_modules[\\/](element-plus|@element-plus)[\\/]/,
              priority: 35,
            },
            {
              name: 'vendor-charts',
              test: /node_modules[\\/](echarts|vue-echarts|zrender)[\\/]/,
              priority: 30,
            },
            {
              name: 'vendor-graph',
              test: /node_modules[\\/](cytoscape|cytoscape-fcose)[\\/]/,
              priority: 30,
            },
            {
              name: 'vendor-tools',
              test: /node_modules[\\/](axios|marked)[\\/]/,
              priority: 25,
            },
            {
              name: 'vendor',
              test: /node_modules[\\/]/,
              priority: 10,
            },
          ],
        },
      },
    },
  },
})
