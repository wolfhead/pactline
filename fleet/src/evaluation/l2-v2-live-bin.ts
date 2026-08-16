import { resolve } from 'node:path'
import { resumeCodexL2V2Execution, resumeCodexL2V2Review, runCodexL2V2Case } from './l2-v2-live.js'

const caseFlag = process.argv.findIndex(value => value === '--case')
const caseId = caseFlag >= 0 ? process.argv[caseFlag + 1] : process.env.PACTLINE_FLEET_L2_CASE
if (caseId === undefined || !/^L2V2-[0-9]{2}$/.test(caseId)) {
  throw new Error('Use --case L2V2-NN or set PACTLINE_FLEET_L2_CASE')
}
const resumeFlag = process.argv.findIndex(value => value === '--resume')
const resume = resumeFlag >= 0 ? process.argv[resumeFlag + 1] : undefined
const executionResumeFlag = process.argv.findIndex(value => value === '--resume-execution')
const executionResume = executionResumeFlag >= 0 ? process.argv[executionResumeFlag + 1] : undefined
if (resume !== undefined && executionResume !== undefined) throw new Error('Choose only one resume mode')
const claimFlag = process.argv.findIndex(value => value === '--claim')
const claim = claimFlag >= 0 ? process.argv[claimFlag + 1] : undefined
const result = executionResume !== undefined
  ? await resumeCodexL2V2Execution({ caseId, runDirectory: resolve(executionResume), ...(claim === undefined ? {} : { existingClaimId: claim }) })
  : resume !== undefined
    ? await resumeCodexL2V2Review({ caseId, runDirectory: resolve(resume), ...(claim === undefined ? {} : { existingClaimId: claim }) })
    : await runCodexL2V2Case({ caseId })
process.stdout.write(`${JSON.stringify(result, null, 2)}\n`)
