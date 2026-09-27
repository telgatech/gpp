import { defineConfig } from 'vitepress'

export default defineConfig({
  lang: 'en-US',
  title: 'Go++',
  description: 'A practical, modern superset of Go.',
  base: process.env.GITHUB_ACTIONS ? '/gpp/' : '/',
  cleanUrls: true,
  lastUpdated: true,
  ignoreDeadLinks: true,
  themeConfig: {
    logo: {
      src: '/go-gopher-nav.png',
      alt: 'Go++ gopher face and cape in a circular badge'
    },
    siteTitle: false,
    nav: [
      { text: 'Features', link: '/features/' },
      { text: 'Guide', link: '/guide/getting-started' },
      { text: 'Language', link: '/guide/language' },
      { text: 'Standard library', link: '/guide/standard-library' },
      { text: 'Tooling', link: '/guide/tooling' },
      { text: 'Specifications', link: '/reference/specifications' },
      { text: 'About', link: '/about' }
    ],
    sidebar: {
      '/features/': [
        {
          text: 'Core language changes',
          items: [
            { text: 'Classes', link: '/features/classes' },
            { text: 'Polymorphism', link: '/features/polymorphism' },
            { text: 'Overloading', link: '/features/overloading' },
            { text: 'Multiple inheritance', link: '/features/multiple-inheritance' },
            { text: 'Exception handling', link: '/features/exceptions' },
            { text: 'Extension methods', link: '/features/extensions' },
            { text: 'Serialization', link: '/features/serialization' },
            { text: 'Class construction', link: '/features/construction' },
            { text: 'Static methods', link: '/features/static-methods' },
            { text: 'Named arguments and defaults', link: '/features/named-arguments' },
            { text: 'Structural records', link: '/features/records' },
            { text: 'Enums', link: '/features/enums' },
            { text: 'String interpolation', link: '/features/interpolation' },
            { text: 'Lambdas', link: '/features/lambdas' },
            { text: 'Regular expressions', link: '/features/regular-expressions' },
            { text: 'Safe access', link: '/features/safe-access' },
            { text: 'Lazy error fallback', link: '/features/error-fallback' },
            { text: 'Annotations and introspection', link: '/features/metadata' }
          ]
        },
        {
          text: 'Application features',
          items: [
            { text: 'Typed templates', link: '/features/templates' },
            { text: 'Embedded assets', link: '/features/embedded-assets' },
            { text: 'HTTP servers and routes', link: '/features/http' },
            { text: 'OpenAPI, Swagger, and OAuth', link: '/features/api-documentation' },
            { text: 'ORM and SQL', link: '/features/orm' },
            { text: 'Suite-based tests', link: '/features/testing' },
            { text: 'External template reloads', link: '/features/template-reload' }
          ]
        },
        {
          text: 'Go workflow and tools',
          items: [
            { text: 'Go compatibility', link: '/features/packages' },
            { text: 'Mixed Go and Go++ builds', link: '/features/mixed-go' },
            { text: 'Project CLI', link: '/features/project-cli' },
            { text: 'Formatter', link: '/features/formatter' },
            { text: 'Source documentation', link: '/features/source-docs' },
            { text: 'Language server', link: '/features/language-server' }
          ]
        }
      ],
      '/guide/': [
        {
          text: 'Start here',
          items: [
            { text: 'Getting started', link: '/guide/getting-started' },
            { text: 'Language tour', link: '/guide/language' },
            { text: 'Standard library', link: '/guide/standard-library' },
            { text: 'Tooling', link: '/guide/tooling' }
          ]
        }
      ],
      '/reference/': [
        {
          text: 'Reference',
          items: [
            { text: 'Specifications', link: '/reference/specifications' },
            { text: 'Compiler architecture', link: '/reference/compiler-architecture' }
          ]
        }
      ]
    },
    outline: 'deep',
    socialLinks: [
      { icon: 'github', link: 'https://github.com/telgatech/gpp' }
    ],
    search: {
      provider: 'local'
    },
    footer: {
      message: 'Built with Go++ and VitePress.',
      copyright: 'Copyright © 2026 Telga Technologies'
    }
  }
})
