import { defineConfig } from 'vitepress'

const siteDescription = 'A superset of Go with modern language features including classes, inheritance, generics, function overloading, named arguments, exceptions, extension methods, annotations, records, enums and more!'

export default defineConfig({
  lang: 'en-US',
  title: 'GPP',
  description: siteDescription,
  base: '/',
  cleanUrls: true,
  lastUpdated: true,
  ignoreDeadLinks: true,
  head: [
    [
      'script',
      {
        type: 'module',
        src: 'https://static.cloudflareinsights.com/beacon.min.js',
        'data-cf-beacon': '{"token":"e077fd4b577243b9a7c9fdeed275427d"}'
      }
    ]
  ],
  transformHead({ pageData }) {
    const relativePath = pageData.relativePath
      .replace(/(^|\/)index\.md$/, '$1')
      .replace(/\.md$/, '')
    const pageUrl = new URL(relativePath, 'https://gpp.telga.in/').href
    const title = pageData.title ? `${pageData.title} | GPP` : 'GPP'
    const description = pageData.description || siteDescription
    const imageUrl = 'https://gpp.telga.in/logo.png'

    return [
      ['meta', { property: 'og:type', content: 'website' }],
      ['meta', { property: 'og:site_name', content: 'GPP' }],
      ['meta', { property: 'og:title', content: title }],
      ['meta', { property: 'og:description', content: description }],
      ['meta', { property: 'og:url', content: pageUrl }],
      ['meta', { property: 'og:image', content: imageUrl }],
      ['meta', { property: 'og:image:alt', content: 'GPP logo' }],
      ['meta', { property: 'og:image:width', content: '598' }],
      ['meta', { property: 'og:image:height', content: '741' }],
      ['meta', { name: 'twitter:card', content: 'summary' }],
      ['meta', { name: 'twitter:title', content: title }],
      ['meta', { name: 'twitter:description', content: description }],
      ['meta', { name: 'twitter:image', content: imageUrl }],
      ['meta', { name: 'twitter:image:alt', content: 'GPP logo' }]
    ]
  },
  themeConfig: {
    logo: {
      src: '/logo.png',
      alt: 'GPP logo'
    },
    siteTitle: false,
    nav: [
      { text: 'Features', link: '/features/' },
      { text: 'Guide', link: '/guide/getting-started' },
      { text: 'Examples', link: '/examples/' },
      { text: 'Language', link: '/guide/language' },
      { text: 'Standard library', link: '/guide/standard-library' },
      { text: 'Tooling', link: '/guide/tooling' },
      { text: 'Specifications', link: '/reference/specifications' },
      { text: 'Changelog', link: 'https://github.com/telgatech/gpp/blob/main/CHANGELOG.md' },
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
            { text: 'Program exit hook', link: '/features/at-exit' },
            { text: 'Extension methods', link: '/features/extensions' },
            { text: 'Serialization', link: '/features/serialization' },
            { text: 'Static methods', link: '/features/static-methods' },
            { text: 'Named arguments and defaults', link: '/features/named-arguments' },
            { text: 'Anonymous structs', link: '/features/structs' },
            { text: 'Enums', link: '/features/enums' },
            { text: 'String interpolation', link: '/features/interpolation' },
            { text: 'Lambdas', link: '/features/lambdas' },
            { text: 'Regular expressions', link: '/features/regular-expressions' },
            { text: 'Safe access', link: '/features/safe-access' },
            { text: 'Lazy error fallback', link: '/features/error-fallback' },
            { text: 'Conditional expressions', link: '/features/conditional-expressions' },
            { text: 'Operators', link: '/features/operators' },
            { text: 'Annotations and introspection', link: '/features/annotations' }
          ]
        },
        {
          text: 'Application features',
          items: [
            { text: 'Typed templates', link: '/features/templates' },
            { text: 'Embedded assets', link: '/features/embedded-assets' },
            { text: 'HTTP servers and routes', link: '/features/http' },
            { text: 'OpenAPI, Swagger, and OAuth', link: '/features/api-documentation' },
            { text: 'Typed SQL queries', link: '/features/sql' },
            { text: 'ORM models', link: '/features/orm' },
            { text: 'Suite-based tests', link: '/features/testing' },
            { text: 'External template reloads', link: '/features/template-reload' }
          ]
        },
        {
          text: 'Go workflow and tools',
          items: [
            { text: 'Go compatibility', link: '/features/packages' },
            { text: 'Mixed Go and GPP builds', link: '/features/mixed-go' },
            { text: 'Project CLI', link: '/features/project-cli' },
            { text: 'Formatter', link: '/features/formatter' },
            { text: 'Source documentation', link: '/features/source-docs' },
            { text: 'Language server', link: '/features/language-server' }
          ]
        }
      ],
      '/guide/standard-library': [
        {
          text: 'Built-in standard library',
          items: [
            { text: 'Overview', link: '/guide/standard-library' },
            { text: 'Prelude (implicit)', link: '/guide/standard-library/prelude' },
            { text: 'gpp/cron', link: '/guide/standard-library/cron' },
            { text: 'gpp/sql', link: '/guide/standard-library/sql' },
            { text: 'gpp/orm', link: '/guide/standard-library/orm' },
            {
              text: 'gpp/http',
              items: [
                { text: 'Server, routes, and context', link: '/guide/standard-library/http' },
                { text: 'OpenAPI and Swagger UI', link: '/guide/standard-library/http#openapi-and-swagger-ui' },
                { text: 'OAuth and OIDC', link: '/guide/standard-library/http#oauth-and-oidc' }
              ]
            },
            {
              text: 'gpp/exp',
              link: '/guide/standard-library/exp',
              items: [
                { text: 'http', link: '/guide/standard-library/exp-http' },
                { text: 'queue', link: '/guide/standard-library/exp-queue' }
              ]
            },
            {
              text: 'gpp/encoding',
              items: [
                { text: 'Overview', link: '/guide/standard-library/encoding/' },
                { text: 'JSON', link: '/guide/standard-library/encoding/json' },
                { text: 'YAML', link: '/guide/standard-library/encoding/yaml' },
                { text: 'GOB', link: '/guide/standard-library/encoding/gob' }
              ]
            },
            { text: 'gpp/tpl', link: '/guide/standard-library/templates' },
            { text: 'gpp/test', link: '/guide/standard-library/testing' }
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
      provider: 'local',
      options: {
        miniSearch: {
          options: {
            tokenize: (text: string) => text.match(/\?\?|[\p{L}\p{N}_]+/gu) ?? []
          }
        }
      }
    },
    footer: {
      message: 'Built with <a href="https://vitepress.dev/">VitePress</a>',
      copyright: 'Copyright © 2026 <a href="https://telga.in">Telga Technologies Pvt Ltd</a>'
    }
  }
})
