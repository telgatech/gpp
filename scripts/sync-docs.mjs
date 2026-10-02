import { mkdir, readdir, readFile, rm, writeFile } from 'node:fs/promises'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')
const sourceDir = path.join(root, 'spec')
const destinationDir = path.join(root, 'docs', 'reference', 'specifications')

await rm(destinationDir, { recursive: true, force: true })
await mkdir(destinationDir, { recursive: true })

const names = (await readdir(sourceDir))
  .filter((name) => name.endsWith('.md'))
  .sort((left, right) => left.localeCompare(right))

function escapeRawHTML(source) {
  let fenced = false
  return source
    .split('\n')
    .map((line) => {
      if (/^\s*```/.test(line)) {
        fenced = !fenced
        return line
      }
      return fenced ? line : line.replaceAll('<', '&lt;')
    })
    .join('\n')
}

function escapeVueInterpolation(source) {
  let fenced = false
  let fenceChar = ''
  return source
    .split('\n')
    .map((line) => {
      const fence = line.match(/^\s*(`{3,}|~{3,})/)
      if (fence) {
        if (!fenced) {
          fenced = true
          fenceChar = fence[1][0]
        } else if (fence[1][0] === fenceChar) {
          fenced = false
          fenceChar = ''
        }
        return line
      }
      if (fenced) return line

      const codeSpans = []
      const protectedLine = line.replace(/(`+)(.+?)\1/g, (span) => {
        const token = `\u0000${codeSpans.length}\u0000`
        const content = span.replace(/^`+|`+$/g, '')
        if (content.includes('{{') || content.includes('}}')) {
          const escapedContent = content
            .replaceAll('&', '&amp;')
            .replaceAll('<', '&lt;')
            .replaceAll('>', '&gt;')
          codeSpans.push(`<code v-pre>${escapedContent}</code>`)
        } else {
          codeSpans.push(span)
        }
        return token
      })
      return protectedLine
        .replaceAll('{{', '&#123;&#123;')
        .replaceAll('}}', '&#125;&#125;')
        .replace(/\u0000(\d+)\u0000/g, (_, index) => codeSpans[Number(index)])
    })
    .join('\n')
}

for (const name of names) {
  const source = escapeRawHTML((await readFile(path.join(sourceDir, name), 'utf8')))
    .replaceAll('```gpp', '```go')
    .replaceAll('```gotemplate', '```go')
  const escapedSource = escapeVueInterpolation(source)
  await writeFile(path.join(destinationDir, name), escapedSource)
}

console.log(`Synced ${names.length} specifications into docs/reference/specifications`)
