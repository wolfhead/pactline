/**
 * Compatibility surface: this root export set predates the Run-domain
 * hardening. Keep it stable until a separately approved compatibility review;
 * new internal coordination types should not be added here by default.
 */
export { runFleetCLI } from './cli.js'
export type { FleetCLIIO } from './cli.js'
export { runFleetServe } from './commands/serve.js'
export type { FleetServeOptions } from './commands/serve.js'
export { FleetService } from './service/fleet-service.js'
export type { FleetServiceOptions } from './service/fleet-service.js'
export { parseFleetConfig, loadFleetConfig, fleetConfigRevision } from './config/load.js'
export { FleetConfigManager } from './config/manager.js'
export type {
  FleetConfigSnapshot,
  FleetDefinitionConfig,
  FleetRouteConfig,
  FleetRoutingConfig,
  FleetServiceConfig,
} from './config/types.js'
export { FleetRegistry } from './registry/fleet-registry.js'
export type {
  FleetRunEventRecord, FleetRunListOptions, FleetRunRecord, FleetRunStage, FleetRunState,
} from './registry/fleet-registry.js'
export type { FleetServiceHealth } from './health/model.js'
export { FleetObservationProjector } from './observation/projection.js'
export type { FleetObservationSource } from './observation/projection.js'
export type {
  ObservationAdapter, ObservationAttention, ObservationEffect, ObservationEnvelope,
  ObservationFleet, ObservationList, ObservationOverview, ObservationRoute,
  ObservationRunDetail, ObservationRunSummary, ObservationTimelineItem,
} from './observation/model.js'
export { runFleetDoctor, isSupportedNodeVersion } from './commands/doctor.js'
export type { FleetDoctorOptions, FleetDoctorResult } from './commands/doctor.js'
export { runDeepSeekDoctor } from './commands/deepseek-doctor.js'
export type { DeepSeekDoctorOptions, DeepSeekDoctorResult } from './commands/deepseek-doctor.js'
export { fleetVersion } from './commands/version.js'
export type { FleetVersionResult } from './commands/version.js'
export { launchFleetUI, runFleetUI } from './commands/ui.js'
export type { FleetUIOptions, FleetUIResult } from './commands/ui.js'
export type {
  HarnessAdapter, HarnessCapabilities, HarnessProbeRequest, HarnessRunEvent, HarnessRunObserver, HarnessSessionReference,
  HarnessRunPolicy, HarnessRunRequest, HarnessSandbox, HarnessStage,
} from './core/harness-adapter.js'
export type {
  CriterionIdentity, CriterionProposal, ExecutionProposal, HarnessEventSummary, HarnessProposal,
  HarnessRecommendation, HarnessRunResult, HarnessTerminalState, HarnessTokenUsage, ModelProvenance,
  ProposalValidationContext, ResolutionAnalysisProposal, ResolutionRequestProposal, ReviewFinding,
  ReviewProposal, VerificationProposal,
} from './core/harness-result.js'
export { proposalResultSchema, validateHarnessProposal } from './core/harness-result.js'
export { RuntimeAdmissionError, StaticRuntimeRouter, requiredCapabilities, requiredSandbox } from './core/runtime-router.js'
export type { AdmittedRuntime, RuntimeRoute, RuntimeRoutes } from './core/runtime-router.js'
export type { FleetWorkDefinition } from './core/work-definition.js'
export { runCandidateImport, runClaimStage } from './core/claim-stage.js'
export type { CandidateImportOptions, ClaimStageDispatch, ClaimStageOptions, ClaimStageResult, ClaimWorkflowStage } from './core/claim-stage.js'
export { PactlineCLI, PactlineClientError, REQUIRED_PACTLINE_FEATURES } from './pactline/client.js'
export type { PactlineCallOptions, PactlineCLIConfig, PactlinePreflightOptions } from './pactline/client.js'
export { resolveTypedIssue } from './pactline/settlement.js'
export type { ResolvedIssueAuthority, ResolveTypedIssueOptions } from './pactline/settlement.js'
export { prepareWorkspace, removeWorkspace, verifyWorkspace } from './repository/workspace.js'
export type { FleetWorkspace, PrepareWorkspaceOptions, RepositoryRevision, WorkspaceMode } from './repository/workspace.js'
export { validateRepositoryDelivery } from './repository/delivery.js'
export type { RepositoryDelivery, RepositoryIdentity, RepositoryProvider } from './repository/delivery.js'
export { DeepSeekHarnessAdapter, deepSeekAdapterPolicy, deepSeekChildEnvironment } from './adapters/deepseek/deepseek-adapter.js'
export { CodexHarnessAdapter, codexAdapterPolicy, codexChildEnvironment } from './adapters/codex/codex-adapter.js'
export type { DeepSeekHarnessAdapterOptions } from './adapters/deepseek/deepseek-adapter.js'
export { resolveDeepSeekCredential } from './adapters/deepseek/credential.js'
export { runDeepSeekL1 } from './evaluation/deepseek-l1-live.js'
export type { DeepSeekL1Options, DeepSeekL1Result } from './evaluation/deepseek-l1-live.js'
export { preflightM54Usability } from './evaluation/m5-4-usability.js'
export type { M54UsabilityPreflightOptions, M54UsabilityPreflightResult } from './evaluation/m5-4-usability.js'
export {
  runM54DeterministicCorrection,
  runM54DeterministicUsability,
  runM54LiveWorkflow,
  runM54RestartRecovery,
} from './evaluation/m5-4-deterministic.js'
export type {
  M54CorrectionResult,
  M54DeterministicResult,
  M54LiveResult,
  M54RestartResult,
} from './evaluation/m5-4-deterministic.js'
