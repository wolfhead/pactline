import { lstat, mkdtemp, readFile, rm, stat, symlink, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { afterEach, describe, expect, it } from 'vitest'
import { assertPrivateFileTarget, ensurePrivateDirectory } from '../../src/service/state-directory.js'

const directories: string[] = []

afterEach(async () => {
  await Promise.all(directories.splice(0).map(path => rm(path, { recursive: true, force: true })))
})

describe('private Fleet state paths', () => {
  it('creates and tightens a private directory', async () => {
    const parent = await mkdtemp(join(tmpdir(), 'fleet-private-state-'))
    directories.push(parent)
    const state = join(parent, 'state')

    await expect(ensurePrivateDirectory(state)).resolves.toBe(state)

    expect((await stat(state)).mode & 0o777).toBe(0o700)
  })

  it('rejects a symbolic-link state directory', async () => {
    const parent = await mkdtemp(join(tmpdir(), 'fleet-private-link-'))
    directories.push(parent)
    const target = join(parent, 'target')
    const linked = join(parent, 'linked')
    await ensurePrivateDirectory(target)
    await symlink(target, linked)

    await expect(ensurePrivateDirectory(linked)).rejects.toThrow('must not be a symbolic link')
  })

  it('rejects a symbolic-link registry file', async () => {
    const parent = await mkdtemp(join(tmpdir(), 'fleet-private-file-'))
    directories.push(parent)
    const real = join(parent, 'real.sqlite')
    const linked = join(parent, 'linked.sqlite')
    await writeFile(real, 'not-a-database')
    await symlink(real, linked)

    await expect(assertPrivateFileTarget(linked)).rejects.toThrow('must not be a symbolic link')
    expect(await readFile(real, 'utf8')).toBe('not-a-database')
    expect((await lstat(linked)).isSymbolicLink()).toBe(true)
  })
})
