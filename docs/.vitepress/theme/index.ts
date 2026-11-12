import DefaultTheme from 'vitepress/theme'
import { h } from 'vue'
import './custom.css'

export default {
  ...DefaultTheme,
  Layout: () => h(DefaultTheme.Layout, null, {
    'layout-top': () => h('div', { class: 'early-release-banner', role: 'status' }, [
      h('strong', 'Early release'),
      h('span', 'GPP is experimental; features and documentation may change.')
    ]),
    'home-hero-actions-after': () => h('p', { class: 'home-brand-notice' }, [
      'GPP is an independent open source project, not affiliated with or endorsed by Google or the Go project.'
    ])
  })
}
