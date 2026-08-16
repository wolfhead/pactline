import { readFile, readdir } from 'node:fs/promises'
import { dirname, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'

const root = resolve(dirname(fileURLToPath(import.meta.url)), '../..')

async function typescriptFiles(directory: string): Promise<string[]> {
  const entries = await readdir(directory, { withFileTypes: true })
  const files: string[] = []
  for (const entry of entries) {
    const path = join(directory, entry.name)
    if (entry.isDirectory()) files.push(...await typescriptFiles(path))
    else if (entry.isFile() && entry.name.endsWith('.ts')) files.push(path)
  }
  return files.sort()
}

describe('Fleet Core architecture boundary', () => {
  it('does not import Harness implementations or vendor SDKs', async () => {
    const files = await typescriptFiles(join(root, 'src/core'))
    expect(files.length).toBeGreaterThan(0)
    for (const file of files) {
      const source = await readFile(file, 'utf8')
      const specifiers = [...source.matchAll(/(?:\bfrom\s+|\bimport\s*(?:\(\s*)?)["']([^"']+)["']/g)]
        .map(match => match[1])
      expect(specifiers, file).not.toEqual(expect.arrayContaining([
        expect.stringMatching(/@deepseek-ai|@openai\/codex|cordis|(?:^|\/)(?:adapters|evaluation)(?:\/|$)/i),
      ]))
    }
  })

  it('has no Harness runtime dependency in the package manifest', async () => {
    const manifest = JSON.parse(await readFile(join(root, 'package.json'), 'utf8')) as Record<string, unknown>
    const serialized = JSON.stringify({ dependencies: manifest.dependencies, peerDependencies: manifest.peerDependencies })
    expect(serialized).not.toMatch(/@deepseek-ai|@openai\/codex|cordis/i)
  })

  it('keeps Pactline lifecycle and repository delivery authority out of Adapters', async () => {
    const files = await typescriptFiles(join(root, 'src/adapters'))
    for (const file of files) {
      const source = await readFile(file, 'utf8')
      expect(source, file).not.toMatch(/PACTLINE_TOKEN|GITHUB_TOKEN|GITLAB_TOKEN|claimTask|settleExecution|settleReview|linkCodeChange|git\s+push/)
      const specifiers = [...source.matchAll(/(?:\bfrom\s+|\bimport\s*(?:\(\s*)?)["']([^"']+)["']/g)]
        .map(match => match[1])
      expect(specifiers, file).not.toEqual(expect.arrayContaining([
        expect.stringMatching(/(?:^|\/)(?:pactline|repository)(?:\/|$)/i),
      ]))
    }
  })

  it('confines DeepSeek and Cordis dependencies to the Adapter-owned runtime closure', async () => {
    const rootManifest = JSON.parse(await readFile(join(root, 'package.json'), 'utf8')) as Record<string, unknown>
    expect(JSON.stringify({ dependencies: rootManifest.dependencies, optionalDependencies: rootManifest.optionalDependencies }))
      .not.toMatch(/@deepseek-ai|cordis/i)

    const runtimeManifest = JSON.parse(await readFile(join(root, 'runtime/deepseek/package.json'), 'utf8')) as {
      dependencies?: Record<string, string>
    }
    expect(Object.keys(runtimeManifest.dependencies ?? {})).toContain('@deepseek-ai/dsh-sdk-jsonrpc-server')
    expect(Object.keys(runtimeManifest.dependencies ?? {})).toContain('@deepseek-ai/dsh-llm-deepseek')
  })

  it('does not import or execute the frozen DeepSeek Bundle from the new Adapter', async () => {
    const files = await typescriptFiles(join(root, 'src/adapters/deepseek'))
    for (const file of files) {
      const source = await readFile(file, 'utf8')
      expect(source, file).not.toMatch(/bundles\/deepseek-fleet|@pactline\/dsh-fleet/)
    }
  })
})
