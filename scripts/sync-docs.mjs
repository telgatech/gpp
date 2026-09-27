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

for (const name of names) {
  const source = escapeRawHTML((await readFile(path.join(sourceDir, name), 'utf8')))
    .replaceAll('```gpp', '```go')
    .replaceAll('```gotemplate', '```go')
    .replaceAll('{{', '&#123;&#123;')
    .replaceAll('}}', '&#125;&#125;')
  await writeFile(path.join(destinationDir, name), source)
}

console.log(`Synced ${names.length} specifications into docs/reference/specifications`)
