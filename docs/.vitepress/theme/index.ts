import DefaultTheme from 'vitepress/theme'
import { h } from 'vue'
import './custom.css'

export default {
  ...DefaultTheme,
  Layout: () => h(DefaultTheme.Layout, null, {
    'layout-top': () => h('div', { class: 'early-release-banner', role: 'status' }, [
      h('strong', 'Early release'),
      h('span', 'Go++ is experimental; features and documentation may change.')
    ])
  })
}
