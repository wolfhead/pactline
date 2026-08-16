import { dirname, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { loadL2V2Spec } from './l2-v2-spec.js'
import { provisionL2V2 } from './l2-v2-provision.js'

const moduleDirectory = dirname(fileURLToPath(import.meta.url))
const fleetRoot = resolve(moduleDirectory, '../..')
const spec = await loadL2V2Spec(join(fleetRoot, 'evaluation/cases/l2-v2.json'))
const manifest = await provisionL2V2(spec)
process.stdout.write(`${JSON.stringify({
  status: manifest.status,
  projectNumber: manifest.pactline?.projectNumber,
  taskNumbers: manifest.cases.map(item => item.taskNumber),
  seededDraftPullRequests: manifest.repository.seededDraftPullRequests,
}, null, 2)}\n`)
