import { createHash } from 'node:crypto'
import { readFile, readdir } from 'node:fs/promises'
import { dirname, join, relative, resolve, sep } from 'node:path'
import { fileURLToPath } from 'node:url'

const fleetRoot = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const repositoryRoot = resolve(fleetRoot, '..')
const baselinePath = join(fleetRoot, 'evaluation/baselines/deepseek-fleet-m0.json')
const baseline = JSON.parse(await readFile(baselinePath, 'utf8'))
const sourceRoot = resolve(repositoryRoot, baseline.source.path)
const excluded = new Set(baseline.source.excluded)

async function filesUnder(directory) {
  const paths = []
  for (const entry of await readdir(directory, { withFileTypes: true })) {
    if (excluded.has(entry.name)) continue
    const path = join(directory, entry.name)
    if (entry.isDirectory()) paths.push(...await filesUnder(path))
    else if (entry.isFile()) paths.push(path)
  }
  return paths
}

const files = (await filesUnder(sourceRoot)).sort((left, right) => left === right ? 0 : left < right ? -1 : 1)
const aggregate = createHash('sha256')
for (const path of files) {
  const digest = createHash('sha256').update(await readFile(path)).digest('hex')
  const name = `./${relative(sourceRoot, path).split(sep).join('/')}`
  aggregate.update(`${digest}  ${name}\n`)
}
const actual = aggregate.digest('hex')
const ok = files.length === baseline.source.file_count && actual === baseline.source.aggregate_sha256
process.stdout.write(`${JSON.stringify({
  ok,
  source: baseline.source.path,
  file_count: files.length,
  aggregate_sha256: actual,
})}\n`)
if (!ok) process.exitCode = 1
