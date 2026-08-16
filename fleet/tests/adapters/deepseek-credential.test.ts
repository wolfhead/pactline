import { chmod, mkdtemp, rm, symlink, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { afterEach, describe, expect, it } from 'vitest'
import { resolveDeepSeekCredential } from '../../src/adapters/deepseek/credential.js'

describe('DeepSeek managed credential boundary', () => {
  const directories: string[] = []
  afterEach(async () => { for (const directory of directories.splice(0)) await rm(directory, { recursive: true, force: true }) })

  async function credential(content: string, mode = 0o600): Promise<string> {
    const directory = await mkdtemp(join(tmpdir(), 'pactline-fleet-credential-'))
    directories.push(directory)
    const path = join(directory, 'credentials.yml')
    await writeFile(path, content, { mode })
    await chmod(path, mode)
    return path
  }

  it('selects only DEEPSEEK_API_KEY from a private YAML document', async () => {
    const path = await credential('DEEPSEEK_API_KEY: model-only-value\nGITHUB_TOKEN: must-not-be-read\n')
    await expect(resolveDeepSeekCredential({ DSH_CREDENTIALS_FILE: path })).resolves.toBe('model-only-value')
  })

  it('prefers a direct process-local key without reading the document', async () => {
    await expect(resolveDeepSeekCredential({
      DEEPSEEK_API_KEY: 'direct-value', DSH_CREDENTIALS_FILE: '/does/not/exist',
    })).resolves.toBe('direct-value')
  })

  it('rejects broad permissions and symlinks', async () => {
    const broad = await credential('DEEPSEEK_API_KEY: value\n', 0o644)
    await expect(resolveDeepSeekCredential({ DSH_CREDENTIALS_FILE: broad })).rejects.toThrow('group or others')

    const target = await credential('DEEPSEEK_API_KEY: value\n')
    const link = `${target}.link`
    await symlink(target, link)
    await expect(resolveDeepSeekCredential({ DSH_CREDENTIALS_FILE: link })).rejects.toThrow('non-symlink')
  })

  it('treats an absent default document as keyless preflight capability', async () => {
    await expect(resolveDeepSeekCredential({ HOME: '/definitely/not/a/real/home' })).resolves.toBeUndefined()
  })
})
