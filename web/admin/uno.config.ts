import { defineConfig, presetIcons, presetUno, transformerDirectives } from 'unocss'

// UnoCSS 原子类与图标：图标按需生成（仅编译用到的 i-ep-* 类），避免全量打包。
// 品牌色/边框/阴影与 styles/tokens.css 中的 CSS 变量保持一致，保证 Element Plus 主题同源。
export default defineConfig({
  presets: [
    presetUno(),
    presetIcons({
      scale: 1.1,
      warn: true,
      collections: {
        ep: () => import('@iconify-json/ep/icons.json').then((m) => m.default),
      },
    }),
  ],
  transformers: [transformerDirectives()],
  theme: {
    colors: {
      brand: {
        DEFAULT: 'var(--brand-600)',
        50: 'var(--brand-50)',
        100: 'var(--brand-100)',
        200: 'var(--brand-200)',
        300: 'var(--brand-300)',
        400: 'var(--brand-400)',
        500: 'var(--brand-500)',
        600: 'var(--brand-600)',
        700: 'var(--brand-700)',
      },
      line: 'var(--line)',
      'line-strong': 'var(--line-strong)',
    },
    boxShadow: {
      card: 'var(--shadow-card)',
      'card-hover': 'var(--shadow-card-hover)',
      pop: 'var(--shadow-pop)',
    },
  },
  shortcuts: {
    // 常用组合：卡片、页面容器、表单操作区
    'app-card': 'bg-white rounded-lg border border-line shadow-card',
    'app-page': 'space-y-4',
  },
})
