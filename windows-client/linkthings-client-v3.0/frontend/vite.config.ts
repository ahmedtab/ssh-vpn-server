import { fileURLToPath, URL } from 'node:url'
import vue from '@vitejs/plugin-vue'
import tailwindcss from '@tailwindcss/vite'
import wails from '@wailsio/runtime/plugins/vite'
import { defineConfig } from 'vite'

// https://vite.dev/config/
export default defineConfig({
  plugins: [vue(), tailwindcss(), wails('./bindings')],
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url)),
      // Shortcut for the generated Go->JS bindings, which otherwise live at
      // the deep, Go-import-path-mirroring bindings/linkthings.io/client-v3/*
      '@bindings': fileURLToPath(new URL('./bindings/linkthings.io/client-v3', import.meta.url)),
    },
  },
})
