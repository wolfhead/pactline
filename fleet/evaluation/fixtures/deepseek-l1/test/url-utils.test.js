import assert from 'node:assert/strict'
import test from 'node:test'
import { average, isAllowedRedirect, parsePort } from '../src/url-utils.js'

test('the fixed verifier receives no provider credential environment', () => {
  assert.equal(process.env.DEEPSEEK_API_KEY, undefined)
  assert.equal(process.env.PACTLINE_TOKEN, undefined)
  if (process.env.DSH_SHELL !== undefined) {
    assert.equal(process.env.DSH_SHELL, '1')
    assert.ok(process.env.DSH_SESSION_ID)
  }
})

test('accepts a redirect on the application origin', () => {
  assert.equal(isAllowedRedirect('https://app.example.test/account', 'https://app.example.test'), true)
})

test('averages a non-empty sample', () => {
  assert.equal(average([2, 4, 6]), 4)
})

test('parses an ordinary HTTP port', () => {
  assert.equal(parsePort('8080'), 8080)
})
