import { chmod, lstat, mkdir, open } from 'node:fs/promises'
import { dirname, resolve } from 'node:path'

function missing(error: unknown): boolean {
  return typeof error === 'object' && error !== null && 'code' in error
    && (error as { code?: unknown }).code === 'ENOENT'
}

function exists(error: unknown): boolean {
  return typeof error === 'object' && error !== null && 'code' in error
    && (error as { code?: unknown }).code === 'EEXIST'
}

export async function ensurePrivateDirectory(path: string): Promise<string> {
  const absolute = resolve(path)
  let existing
  try {
    existing = await lstat(absolute)
  } catch (error) {
    if (!missing(error)) throw error
  }
  if (existing?.isSymbolicLink() === true) throw new Error(`Private directory must not be a symbolic link: ${absolute}`)
  if (existing !== undefined && !existing.isDirectory()) throw new Error(`Private path is not a directory: ${absolute}`)
  await mkdir(absolute, { recursive: true, mode: 0o700 })
  await chmod(absolute, 0o700)
  return absolute
}

export async function assertPrivateFileTarget(path: string): Promise<string> {
  const absolute = resolve(path)
  await ensurePrivateDirectory(dirname(absolute))
  try {
    const existing = await lstat(absolute)
    if (existing.isSymbolicLink()) throw new Error(`Private file must not be a symbolic link: ${absolute}`)
    if (!existing.isFile()) throw new Error(`Private path is not a regular file: ${absolute}`)
  } catch (error) {
    if (!missing(error)) throw error
  }
  return absolute
}

export async function preparePrivateFile(path: string): Promise<string> {
  const absolute = await assertPrivateFileTarget(path)
  try {
    const handle = await open(absolute, 'wx', 0o600)
    await handle.close()
  } catch (error) {
    if (!exists(error)) throw error
    await assertPrivateFileTarget(absolute)
  }
  await chmod(absolute, 0o600)
  return absolute
}
