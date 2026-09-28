import { mkdir, readFile, readdir, rm, writeFile } from 'node:fs/promises'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')
const sourceDir = path.join(root, 'examples')
const destinationDir = path.join(root, 'docs', 'examples')

const readme = await readFile(path.join(sourceDir, 'README.md'), 'utf8')
const descriptions = new Map()
let currentDescription = ''
let currentName = ''
for (const line of readme.split('\n')) {
  const match = line.match(/^- `([^`]+)` — (.*)$/)
  if (match) {
    if (currentName) descriptions.set(currentName, currentDescription.trim())
    currentName = match[1]
    currentDescription = match[2]
  } else if (currentName && /^\s{2,}\S/.test(line)) {
    currentDescription += ` ${line.trim()}`
  } else if (line.trim() && currentName) {
    descriptions.set(currentName, currentDescription.trim())
    currentName = ''
    currentDescription = ''
  }
}
if (currentName) descriptions.set(currentName, currentDescription.trim())

const companionFiles = new Map([
  ['embed.gpp', ['embed_assets/index.html', 'embed_schema.sql']],
  ['tpl.gpp', ['tpl_external.gpp.tpl']],
])

function slug(value) {
  return value.toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-|-$/g, '')
}

function codeFence(source, language) {
  const ticks = Math.max(3, ...[...source.matchAll(/^(`{3,})/gm)].map((match) => match[1].length + 1))
  const fence = '`'.repeat(ticks)
  return `${fence}${language}\n${source.replace(/\n?$/, '\n')}${fence}`
}

function languageFor(file) {
  if (file.endsWith('.sql')) return 'sql'
  if (file.endsWith('.html')) return 'html'
  return 'go'
}

async function writeExamplePage({ name, title, summary, command, files, notes = [] }) {
  const pageSlug = slug(name)
  let page = `# ${title}\n\n${summary}\n\nRun it from the repository root:\n\n\`\`\`bash\n${command}\n\`\`\`\n`
  for (const note of notes) page += `\n${note}\n`
  for (const relativeFile of files) {
    const source = await readFile(path.join(sourceDir, relativeFile), 'utf8')
    page += `\n## \`${relativeFile}\`\n\n${codeFence(source, languageFor(relativeFile))}\n`
  }
  await writeFile(path.join(destinationDir, `${pageSlug}.md`), page)
  return { slug: pageSlug, title, summary }
}

await rm(destinationDir, { recursive: true, force: true })
await mkdir(destinationDir, { recursive: true })

const entries = []
const rootFiles = (await readdir(sourceDir))
  .filter((name) => name.endsWith('.gpp') && name !== 'main.gpp')
  .sort((left, right) => left.localeCompare(right))

for (const file of rootFiles) {
  const relatedFiles = [file, ...(companionFiles.get(file) ?? [])]
  const extra = companionFiles.get(file) ?? []
  const relatedDescription = extra.length
    ? ` The page also includes its companion resource files: ${extra.map((name) => `\`${name}\``).join(', ')}.`
    : ''
  const notes = file === 'serialization.gpp'
    ? ['The YAML portion requires `gopkg.in/yaml.v3`; run `go get gopkg.in/yaml.v3` in the generated module before running the example.']
    : []
  const command = file === 'foo.gpp'
    ? 'gpp build examples/foo.gpp'
    : file === 'testing.gpp'
      ? 'gpp test examples/testing.gpp'
      : `gpp run examples/${file}`
  entries.push(await writeExamplePage({
    name: file,
    title: file,
    summary: `${descriptions.get(file) ?? 'A runnable Go++ source example.'}${relatedDescription}`,
    command,
    files: relatedFiles,
    notes,
  }))
}

entries.push(await writeExamplePage({
  name: 'packages',
  title: 'Dotted packages',
  summary: 'A two-file example of logical dotted packages and a qualified cross-package class constructor.',
  command: 'gpp examples/packages/people.gpp examples/packages/main.gpp\n(cd .gpp && go run .)',
  files: ['packages/people.gpp', 'packages/main.gpp'],
  notes: ['Pass `-module` if the example imports a generated package under a different module path.'],
}))

for (const project of ['go_from_gpp', 'go_imports_gpp']) {
  const directory = `mixed/${project}`
  const files = (await readdir(path.join(sourceDir, directory)))
    .filter((name) => !name.startsWith('.'))
    .sort((left, right) => left.localeCompare(right))
    .map((name) => `${directory}/${name}`)
  entries.push(await writeExamplePage({
    name: directory,
    title: project === 'go_from_gpp' ? 'Go++ calling Go' : 'Go importing Go++',
    summary: descriptions.get('mixed/') ?? 'A complete mixed-language project showing Go and Go++ interoperability.',
    command: `gpp run examples/mixed/${project}`,
    files,
  }))
}

let index = `# Runnable examples\n\nThese examples are copied directly from the repository source during the docs build. Each page includes complete source files with copy buttons and the command to run the example from a repository checkout.\n\n`
index += `## Standalone examples\n\n`
for (const entry of entries.filter((item) => !['packages', 'mixed-go-from-gpp', 'mixed-go-imports-gpp'].includes(item.slug))) {
  index += `- [\`${entry.title}\`](/examples/${entry.slug}) — ${entry.summary}\n`
}
index += `\n## Multi-file projects\n\n`
for (const entry of entries.filter((item) => ['packages', 'mixed-go-from-gpp', 'mixed-go-imports-gpp'].includes(item.slug))) {
  index += `- [${entry.title}](/examples/${entry.slug}) — ${entry.summary}\n`
}
await writeFile(path.join(destinationDir, 'index.md'), index)

console.log(`Generated ${entries.length} runnable example pages in docs/examples`)
