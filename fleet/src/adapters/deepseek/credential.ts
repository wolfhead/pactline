import { lstat, readFile } from 'node:fs/promises'
import { homedir } from 'node:os'
import { join, resolve } from 'node:path'
import { parseDocument } from 'yaml'

const MAX_CREDENTIAL_FILE_BYTES = 65_536

function missing(error: unknown): boolean {
  return error instanceof Error && (error as NodeJS.ErrnoException).code === 'ENOENT'
}

function usable(value: string): string {
  if (value.trim() === '' || /[\r\n\0]/.test(value)) throw new Error('DeepSeek API key is empty or contains invalid characters')
  return value
}

/** Resolve only the DeepSeek key from an explicitly private DSH credential document. */
export async function resolveDeepSeekCredential(environment: NodeJS.ProcessEnv): Promise<string | undefined> {
  const direct = environment.DEEPSEEK_API_KEY
  if (direct !== undefined && direct.trim() !== '') return usable(direct)

  const explicit = environment.DSH_CREDENTIALS_FILE
  const path = resolve(explicit ?? join(environment.HOME ?? homedir(), '.dsh', '.credentials.yaml'))
  const info = await lstat(path).catch((error: unknown) => {
    if (missing(error) && explicit === undefined) return undefined
    throw new Error('DeepSeek credential document is unavailable', { cause: error })
  })
  if (info === undefined) return undefined
  if (!info.isFile() || info.isSymbolicLink()) throw new Error('DeepSeek credential document must be a regular non-symlink file')
  if ((info.mode & 0o077) !== 0) throw new Error('DeepSeek credential document must not be accessible by group or others')
  if (info.size > MAX_CREDENTIAL_FILE_BYTES) throw new Error('DeepSeek credential document exceeds the size limit')

  const document = parseDocument(await readFile(path, 'utf8'), { prettyErrors: false, uniqueKeys: true })
  if (document.errors.length > 0) throw new Error('DeepSeek credential document is invalid YAML')
  const value = document.toJS() as unknown
  if (typeof value !== 'object' || value === null || Array.isArray(value)) {
    throw new Error('DeepSeek credential document must be a mapping')
  }
  const key = (value as Record<string, unknown>).DEEPSEEK_API_KEY
  if (typeof key !== 'string') throw new Error('DeepSeek credential document does not contain DEEPSEEK_API_KEY')
  return usable(key)
}
