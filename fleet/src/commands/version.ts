import { FLEET_VERSION } from '../version.js'

export interface FleetVersionResult {
  readonly name: '@pactline/fleet'
  readonly version: string
  readonly executable: 'pactline-fleet'
}

export function fleetVersion(): FleetVersionResult {
  return { name: '@pactline/fleet', version: FLEET_VERSION, executable: 'pactline-fleet' }
}
