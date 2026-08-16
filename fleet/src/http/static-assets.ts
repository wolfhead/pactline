import { fileURLToPath } from 'node:url'
import { dirname, resolve } from 'node:path'

/** Resolves both source-tree and packed-package Fleet Web builds. */
export function fleetWebAssetsPath(moduleUrl: string = import.meta.url): string {
  return resolve(dirname(fileURLToPath(moduleUrl)), '../../web/dist')
}
