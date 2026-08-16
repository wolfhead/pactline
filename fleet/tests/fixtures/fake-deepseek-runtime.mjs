import { createInterface } from 'node:readline'

const lines = createInterface({ input: process.stdin, crlfDelay: Number.POSITIVE_INFINITY })
const send = value => { process.stdout.write(`${JSON.stringify(value)}\n`) }

lines.on('line', line => {
  const frame = JSON.parse(line)
  if (frame.method === 'initialize') {
    send({ jsonrpc: '2.0', id: frame.id, result: { serverInfo: { name: 'deepseek-harness-sdk-runtime', version: 'test' } } })
    return
  }
  if (frame.method === 'session/prompt') {
    send({ jsonrpc: '2.0', id: frame.id, result: { messageId: 'fixture-message' } })
    send({ jsonrpc: '2.0', method: 'fixture.notice', params: { value: 1 } })
    return
  }
  if (frame.method === 'shutdown') {
    send({ jsonrpc: '2.0', id: frame.id, result: {} })
    setImmediate(() => process.exit(0))
  }
})
