import { runMixedComparison } from './l2-v2-mixed.js'

const routeFlag = process.argv.findIndex(value => value === '--route')
const route = routeFlag >= 0 ? process.argv[routeFlag + 1] : undefined
if (route !== 'deepseek-codex' && route !== 'codex-deepseek') {
  throw new Error('Use --route deepseek-codex or --route codex-deepseek')
}
process.stdout.write(`${JSON.stringify(await runMixedComparison(route), null, 2)}\n`)
