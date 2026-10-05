import DefaultTheme from 'vitepress/theme'
import { h } from 'vue'
import './custom.css'

export default {
  ...DefaultTheme,
  Layout: () => h(DefaultTheme.Layout, null, {
    'layout-top': () => h('div', { class: 'early-release-banner', role: 'status' }, [
      h('strong', 'Early release'),
      h('span', 'Go++ is experimental; features and documentation may change.')
    ]),
    'home-hero-actions-after': () => h('p', { class: 'home-brand-notice' }, [
      'Go++ is an independent open source project, not affiliated with or endorsed by Google or the Go project. The Go Gopher was created by Renee French and is licensed under ',
      h('a', { href: 'https://creativecommons.org/licenses/by/4.0/' }, 'CC BY 4.0'),
      '; Go++ campaign images are AI-assisted adaptations.'
    ])
  })
}
