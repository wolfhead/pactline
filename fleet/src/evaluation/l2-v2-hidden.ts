import { createHash } from 'node:crypto'
import { cp, mkdir, mkdtemp, readFile, rm, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { dirname, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { runFixedVerification, type CommandObservation } from '../core/verification.js'

const moduleDirectory = dirname(fileURLToPath(import.meta.url))
const fleetRoot = resolve(moduleDirectory, '../..')
const assetRoot = join(fleetRoot, 'evaluation/hidden/l2-v2')

interface HiddenProfile {
  readonly asset: string
  readonly destination: string
  readonly command: string
}

const profiles: Readonly<Record<string, HiddenProfile>> = {
  nullable_schedule_patch: {
    asset: 'nullable_schedule_patch_test.go', destination: 'internal/domain/fleet_hidden_l2_v2_test.go',
    command: 'go test ./internal/domain -run FleetHidden -count=1',
  },
  compact_issue_ordering: {
    asset: 'compact_issue_ordering_test.go', destination: 'internal/application/fleet_hidden_l2_v2_test.go',
    command: 'go test ./internal/application -run FleetHidden -count=1',
  },
  oversized_cli_response: {
    asset: 'oversized_cli_response_test.go', destination: 'internal/cli/fleet_hidden_l2_v2_test.go',
    command: 'go test ./internal/cli -run FleetHidden -count=1',
  },
  claim_stage_outcome_matrix: {
    asset: 'claim_stage_outcome_matrix_test.go', destination: 'internal/domain/fleet_hidden_l2_v2_test.go',
    command: 'go test ./internal/domain -run FleetHidden -count=1',
  },
  clean_schedule_validation: {
    asset: 'clean_schedule_validation_test.go', destination: 'internal/domain/fleet_hidden_l2_v2_test.go',
    command: 'go test ./internal/domain -run FleetHidden -count=1',
  },
  typed_retry_resolution: {
    asset: 'oversized_cli_response_test.go', destination: 'internal/cli/fleet_hidden_l2_v2_test.go',
    command: 'go test ./internal/cli -run FleetHidden -count=1',
  },
}

export interface HiddenVerificationEvidence {
  readonly profile: string
  readonly assetSha256: string
  readonly commands: readonly CommandObservation[]
}

/** Evaluate a post-Agent snapshot in a second temp tree that never reaches the Harness or remote repository. */
export async function runL2V2HiddenVerification(
  workspace: string,
  profileName: string,
  environment: NodeJS.ProcessEnv = process.env,
  expectedOutcome: 'passed' | 'failed' | 'observed' = 'passed',
): Promise<HiddenVerificationEvidence> {
  const profile = profiles[profileName]
  if (profile === undefined) throw new Error(`Unknown L2 v2 hidden verification profile: ${profileName}`)
  const assetPath = join(assetRoot, profile.asset)
  const asset = await readFile(assetPath)
  const root = await mkdtemp(join(tmpdir(), 'pactline-fleet-hidden-'))
  const evaluator = join(root, 'repository')
  try {
    await cp(workspace, evaluator, { recursive: true, force: false, errorOnExist: true })
    const destination = join(evaluator, profile.destination)
    await mkdir(dirname(destination), { recursive: true })
    await writeFile(destination, asset, { flag: 'wx', mode: 0o600 })
    const commands = await runFixedVerification(evaluator, [profile.command], { environment, timeoutMs: 600_000 })
    if (commands.length !== 1 || (expectedOutcome !== 'observed' && commands[0]?.outcome !== expectedOutcome)) {
      throw new Error(`Hidden verification outcome did not match ${expectedOutcome} for ${profileName}`)
    }
    return { profile: profileName, assetSha256: createHash('sha256').update(asset).digest('hex'), commands }
  } finally {
    await rm(root, { recursive: true, force: true })
  }
}
