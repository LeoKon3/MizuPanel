/** @type {import('tailwindcss').Config} */
export default {
  darkMode: 'class',
  content: ['./index.html', './src/**/*.{ts,tsx}'],
  theme: {
    extend: {
      fontFamily: {
        display: ['IBM Plex Sans', 'Noto Sans SC', 'Noto Sans CJK SC', 'Microsoft YaHei', 'PingFang SC', 'WenQuanYi Micro Hei', 'sans-serif'],
        body: ['IBM Plex Sans', 'Noto Sans SC', 'Noto Sans CJK SC', 'Microsoft YaHei', 'PingFang SC', 'WenQuanYi Micro Hei', 'sans-serif']
      },
      colors: {
        background: 'rgb(var(--background) / <alpha-value>)',
        foreground: 'rgb(var(--foreground) / <alpha-value>)',
        surface: 'rgb(var(--surface) / <alpha-value>)',
        'surface-elevated': 'rgb(var(--surface-elevated) / <alpha-value>)',
        card: 'rgb(var(--card) / <alpha-value>)',
        'card-foreground': 'rgb(var(--card-foreground) / <alpha-value>)',
        border: 'rgb(var(--border) / <alpha-value>)',
        muted: 'rgb(var(--muted) / <alpha-value>)',
        'muted-foreground': 'rgb(var(--muted-foreground) / <alpha-value>)',
        primary: 'rgb(var(--primary) / <alpha-value>)',
        'primary-foreground': 'rgb(var(--primary-foreground) / <alpha-value>)',
        success: 'rgb(var(--success) / <alpha-value>)',
        warning: 'rgb(var(--warning) / <alpha-value>)',
        danger: 'rgb(var(--danger) / <alpha-value>)',
        info: 'rgb(var(--info) / <alpha-value>)',
        sidebar: 'rgb(var(--sidebar-background) / <alpha-value>)',
        'sidebar-foreground': 'rgb(var(--sidebar-foreground) / <alpha-value>)',
        'sidebar-border': 'rgb(var(--sidebar-border) / <alpha-value>)',
        'sidebar-active': 'rgb(var(--sidebar-active) / <alpha-value>)',
        'sidebar-active-foreground': 'rgb(var(--sidebar-active-foreground) / <alpha-value>)',
        header: 'rgb(var(--header-background) / <alpha-value>)',
        'header-border': 'rgb(var(--header-border) / <alpha-value>)',
        panel: 'rgb(var(--panel-background) / <alpha-value>)',
        input: 'rgb(var(--input-background) / <alpha-value>)',
        popover: 'rgb(var(--popover-background) / <alpha-value>)',
        code: 'rgb(var(--code-background) / <alpha-value>)',
        'code-foreground': 'rgb(var(--code-foreground) / <alpha-value>)',
        terminal: 'rgb(var(--terminal-background) / <alpha-value>)',
        'terminal-foreground': 'rgb(var(--terminal-foreground) / <alpha-value>)'
      },
      boxShadow: {
        glass: '0 24px 70px rgba(15, 23, 42, 0.10)'
      }
    }
  },
  plugins: []
}
