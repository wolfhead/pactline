import { readFileSync } from 'node:fs'
import { ToolArgsError, validateJsonSchemaValue } from '@deepseek-ai/dsh-tools'

export const name = 'pactline-fleet-result'
export const inject = ['tools']

const TOOL_NAME = 'submit_fleet_result'

function loadSchema() {
  const path = process.env.PACTLINE_FLEET_RESULT_SCHEMA_PATH
  if (path === undefined || path === '') throw new Error('Pactline Fleet result schema path is required')
  const schema = JSON.parse(readFileSync(path, 'utf8'))
  if (schema === null || typeof schema !== 'object' || Array.isArray(schema)) {
    throw new Error('Pactline Fleet result schema must be an object')
  }
  return schema
}

export function apply(ctx) {
  const schema = loadSchema()
  let recorded = false
  ctx.tools.register({
    name: TOOL_NAME,
    description: 'Submit the final Pactline Fleet proposal exactly once. Plain text is not accepted as a result.',
    parameters: schema,
    output: {
      schema: {
        type: 'object',
        properties: { recorded: { type: 'boolean', const: true } },
        required: ['recorded'],
        additionalProperties: false,
      },
      render: () => [{ type: 'text', text: 'Pactline Fleet proposal recorded.' }],
    },
    execute(args, execution) {
      const violations = validateJsonSchemaValue(schema, args)
      if (violations.length > 0) throw new ToolArgsError(violations)
      execution.concludeTurn()
      return Promise.resolve({ recorded: true })
    },
  })
  ctx.tools.guard(execution => recorded
    ? `Pactline Fleet proposal is already recorded; ${execution.name} is not executed`
    : undefined)
  ctx.on('tools/result', function (execution, result) {
    if (execution.name === TOOL_NAME && !result.isError) recorded = true
  })
}
