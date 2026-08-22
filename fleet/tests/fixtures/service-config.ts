export function serviceConfigYAML(options: {
  readonly stateDirectory: string
  readonly firstWorkspace: string
  readonly secondWorkspace?: string
  readonly server?: string
  readonly httpAddress?: string
  readonly httpPort?: number
  readonly firstProject?: number
  readonly secondProject?: number
  readonly firstAdapter?: string
  readonly firstModel?: string
}): string {
  const second = options.secondWorkspace === undefined ? '' : `
  second:
    project: ${String(options.secondProject ?? 12)}
    workspaceRoot: ${options.secondWorkspace}
    routing:
      execution: { adapter: codex, model: gpt-5.6-sol }
      review: { adapter: codex, model: gpt-5.6-sol }
      correction: { adapter: codex, model: gpt-5.6-sol }
      resolutionAnalysis: { adapter: codex, model: gpt-5.6-sol }
`
  return `
version: 1
service:
  pactline:
    server: ${options.server ?? 'http://localhost:8080'}
    tokenEnv: TEST_PACTLINE_TOKEN
    executable: /test/bin/pactline
  stateDirectory: ${options.stateDirectory}
  pollInterval: 5s
  maxConcurrentRuns: 2
  shutdownDeadline: 15s
  http:
    address: ${options.httpAddress ?? '127.0.0.1'}
    port: ${String(options.httpPort ?? 7331)}
fleets:
  first:
    project: ${String(options.firstProject ?? 5)}
    maxConcurrentRuns: 1
    workspaceRoot: ${options.firstWorkspace}
    routing:
      execution: { adapter: ${options.firstAdapter ?? 'codex'}, model: ${options.firstModel ?? 'gpt-5.6-sol'} }
      review: { adapter: codex, model: gpt-5.6-sol }
      correction: { adapter: codex, model: gpt-5.6-sol }
      resolutionAnalysis: { adapter: codex, model: gpt-5.6-sol }
    credentials:
      git: LOCAL_TEST_GIT
      codeChange: LOCAL_TEST_CODE_CHANGE
${second}
`
}
