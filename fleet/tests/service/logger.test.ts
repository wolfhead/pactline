import { describe, expect, it } from 'vitest'
import { JSONFleetLogger } from '../../src/service/logger.js'

describe('JSONFleetLogger', () => {
  it('emits bounded structured events without credential fields', () => {
    let output = ''
    const logger = new JSONFleetLogger(
      { write(value) { output += value } },
      () => new Date('2026-08-15T10:00:00Z'),
    )

    logger.log('warn', 'fleet.test', {
      token: 'hidden-token-value',
      error: 'Bearer hidden-bearer-value',
      nested: { password: 'hidden-password-value', safe: 'visible' },
    })

    expect(JSON.parse(output)).toEqual({
      at: '2026-08-15T10:00:00.000Z',
      level: 'warn',
      event: 'fleet.test',
      error: 'Bearer [REDACTED]',
      nested: { safe: 'visible' },
    })
    expect(output).not.toContain('hidden-')
  })
})
