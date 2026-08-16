import { generateL2V2Report } from './l2-v2-report.js'

process.stdout.write(`${JSON.stringify(await generateL2V2Report(), null, 2)}\n`)
