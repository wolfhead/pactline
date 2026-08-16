import { spawn } from 'node:child_process'
import { platform } from 'node:os'
import { loadFleetConfig } from '../config/load.js'

export interface FleetUIOptions {
  readonly configPath: string
  readonly open?: boolean
  readonly fetchURL?: (url: string) => Promise<{ readonly ok: boolean; readonly status: number }>
  readonly launch?: (url: string) => void
}

export interface FleetUIResult {
  readonly url: string
  readonly serviceReachable: true
  readonly opened: boolean
}

export function launchFleetUI(url: string): void {
  const command = platform() === 'darwin' ? 'open' : platform() === 'win32' ? 'cmd' : 'xdg-open'
  const args = platform() === 'win32' ? ['/c', 'start', '', url] : [url]
  const child = spawn(command, args, { detached: true, stdio: 'ignore', shell: false })
  child.unref()
}

export async function runFleetUI(options: FleetUIOptions): Promise<FleetUIResult> {
  const snapshot = await loadFleetConfig(options.configPath)
  const address = snapshot.config.service.http.address
  const host = address === '::1' ? '[::1]' : address
  const url = `http://${host}:${String(snapshot.config.service.http.port)}`
  const response = await (options.fetchURL ?? (async target => await fetch(target)))(`${url}/livez`)
  if (!response.ok) throw new Error(`Fleet Service is not reachable at ${url} (HTTP ${String(response.status)})`)
  if (options.open === true) (options.launch ?? launchFleetUI)(url)
  return { url, serviceReachable: true, opened: options.open === true }
}
