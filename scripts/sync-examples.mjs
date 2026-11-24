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
  ['foo.gpp', ['foo.bar.gpp']],
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
  .filter((name) => name.endsWith('.gpp') && name !== 'main.gpp' && name !== 'foo.bar.gpp')
  .sort((left, right) => left.localeCompare(right))

for (const file of rootFiles) {
  const relatedFiles = [file, ...(companionFiles.get(file) ?? [])]
  const extra = companionFiles.get(file) ?? []
  const relatedDescription = extra.length
    ? ` The page also includes its companion resource files: ${extra.map((name) => `\`${name}\``).join(', ')}.`
    : ''
  const summary = descriptions.get(file)
  if (!summary) throw new Error(`Missing example description for ${file} in examples/README.md`)
  const notes = file === 'serialization.gpp'
    ? ['The YAML portion requires `gopkg.in/yaml.v3`; run `go get gopkg.in/yaml.v3` in the generated module before running the example.']
    : []
  const command = file === 'foo.gpp'
    ? 'gpp run examples/foo.gpp examples/foo.bar.gpp'
    : file === 'testing.gpp'
      ? 'gpp test examples/testing.gpp'
      : `gpp run examples/${file}`
  entries.push(await writeExamplePage({
    name: file,
    title: file,
    summary: `${summary}${relatedDescription}`,
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
  const directory = `interop/${project}`
  const files = (await readdir(path.join(sourceDir, directory)))
    .filter((name) => !name.startsWith('.'))
    .sort((left, right) => left.localeCompare(right))
    .map((name) => `${directory}/${name}`)
  const command = project === 'go_from_gpp'
    ? `# Runs main.gpp with native.go compiled alongside it.\ngpp run examples/interop/${project}`
    : `# Compiles greeting.gpp, then runs the Go main.go entry point.\ngpp run examples/interop/${project}`
  const summary = project === 'go_from_gpp'
    ? 'Call native Go functions from GPP and catch a Go error as a GPP exception.'
    : 'Import a GPP package from Go, use its Greeting class, and handle returned errors in Go.'
  entries.push(await writeExamplePage({
    name: directory,
    title: project === 'go_from_gpp' ? 'GPP calling Go' : 'Go importing GPP',
    summary,
    command,
    files,
  }))
}

let index = `# Runnable examples\n\nThese examples are copied directly from the repository source during the docs build. Each page includes complete source files with copy buttons and the command to run the example from a repository checkout.\n\n`
const sections = [
  {
    title: 'Language features',
    slugs: [
      'annotation-inheritance-gpp', 'annotations-gpp', 'assignments-gpp', 'classes-gpp',
      'constructors-gpp', 'enums-gpp', 'exceptions-gpp', 'expr-catch-gpp',
      'extension-methods-gpp', 'generics-gpp', 'hello-gpp', 'inheritance-gpp',
      'init-constructors-gpp', 'interfaces-gpp', 'introspection-gpp', 'lambdas-gpp',
      'multi-error-fallback-gpp', 'multi-extension-gpp', 'operators-gpp',
      'or-assignment-gpp', 'overloading-gpp', 'polymorphism-gpp', 'regexp-gpp',
      'safe-access-gpp', 'slices-gpp', 'static-methods-gpp', 'string-interpolation-gpp',
      'structs-gpp',
    ],
  },
  {
    title: 'Packages and interop',
    slugs: [
      'foo-gpp', 'imports-gpp', 'packages', 'interop-go-from-gpp', 'interop-go-imports-gpp',
    ],
  },
  {
    title: 'Standard library',
    slugs: [
      'cache-gpp', 'containers-gpp', 'cron-gpp', 'embed-gpp', 'html-template-gpp',
      'http-gpp', 'oauth-gpp', 'orm-gpp', 'prelude-gpp', 'queue-gpp', 'serialization-gpp',
      'sql-gpp', 'sse-gpp', 'tpl-gpp', 'workflow-gpp',
    ],
  },
  {
    title: 'Applications and testing',
    slugs: ['todo-app-gpp', 'testing-gpp'],
  },
]
const entriesBySlug = new Map(entries.map((entry) => [entry.slug, entry]))
const groupedSlugs = new Set()
for (const section of sections) {
  index += `## ${section.title}\n\n`
  for (const slug of section.slugs) {
    const entry = entriesBySlug.get(slug)
    if (!entry) throw new Error(`Example ${slug} is missing from the generated entries`)
    if (groupedSlugs.has(slug)) throw new Error(`Example ${slug} appears in multiple index sections`)
    groupedSlugs.add(slug)
    const title = entry.title.endsWith('.gpp') ? `\`${entry.title}\`` : entry.title
    index += `- [${title}](/examples/${entry.slug}) — ${entry.summary}\n`
  }
}
const ungrouped = entries.filter((entry) => !groupedSlugs.has(entry.slug))
if (ungrouped.length > 0) throw new Error(`Examples missing an index section: ${ungrouped.map((entry) => entry.slug).join(', ')}`)
await writeFile(path.join(destinationDir, 'index.md'), index)

console.log(`Generated ${entries.length} runnable example pages in docs/examples`)
