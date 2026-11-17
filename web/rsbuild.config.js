import { defineConfig } from '@rsbuild/core'
import { pluginReact } from '@rsbuild/plugin-react'
import tailwindcss from '@tailwindcss/postcss'
import { fileURLToPath } from 'node:url'

export default defineConfig({
  plugins: [pluginReact()],
  source: { entry: { index: './src/main.jsx' } },
  resolve: { alias: { '@': fileURLToPath(new URL('./src', import.meta.url)) } },
  html: { template: './index.html' },
  output: {
    distPath: { js: 'assets', css: 'assets', font: 'assets', svg: 'assets', image: 'assets' },
    sourceMap: { js: false, css: false },
  },
  tools: { postcss: { postcssOptions: { plugins: [tailwindcss()] } } },
})
