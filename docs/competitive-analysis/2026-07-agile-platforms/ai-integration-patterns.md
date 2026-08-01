# AI Integration Patterns in Agile Work Platforms

Research date: 2026-07-31

## Executive conclusion

The important competitive change is not that project-management products can
generate text. It is that several products now treat an AI Agent as a bounded
participant in the delivery system: a person delegates work, a session receives
context and tools, the platform exposes progress and questions, and the result
returns through an ordinary review and delivery boundary.

The five products in this study occupy different layers:

- Worktile uses AI primarily as an in-place planning assistant. It proposes
  decomposition and execution advice; a human edits and confirms resulting
  tasks.
- Azure DevOps exposes governed work and delivery tools through MCP, and can
  hand an Azure Boards item to GitHub Copilot. Its strongest contribution is
  the enterprise identity and permission substrate rather than a native Agent
  conversation model.
- Jira combines organization-wide context, configurable Agents, automation,
  and a coding Agent that produces draft pull requests.
- Linear has the clearest project-management-native Agent domain: responsible
  human, Agent delegate, Agent session, semantic activity, questions, steering,
  repository selection, and coding delivery.
- GitHub has the strongest code-native execution boundary: repository-scoped
  sessions, tools and MCP, hooks, logs, pull requests, CI, security scanning,
  reviews, and merge policies. It also supports both first-party Copilot and
  third-party coding Agents.

For Pactline, the lesson is not to build its own model runner. The stronger
opportunity is to become the provider-neutral control and acceptance plane for
real external coding sessions. The product should standardize delegation,
effective context, session provenance, semantic progress, human decisions,
delivery artifacts, verification evidence, cost, and final acceptance without
requiring execution inside a Pactline-owned sandbox.

## Evaluation framework

This analysis uses eight dimensions. A product that is strong in only one or
two dimensions may have useful AI features but does not yet have a dependable
Agent workflow.

### 1. Trigger and intent boundary

How does execution begin, and how does the product distinguish “explain or
plan” from “change the system”?

Relevant triggers include explicit delegation, mention, comment, chat command,
automation, triage rule, schedule, or external API call. The critical safety
property is that a recommendation does not silently become an execution.

### 2. Context assembly

What information is supplied to the Agent, and can a human inspect the
effective context? Possible sources include the work item, comments,
acceptance criteria, repository, instructions, organizational knowledge,
connected SaaS products, and developer-local environment guidance.

More context is not automatically better. It must be relevant, authorized,
versioned enough to explain the result, and protected from accidental
cross-project disclosure.

### 3. Identity, authority, and accountability

Does the Agent act as the invoking user, as a dedicated application identity,
or as a distinct Agent principal? Which human remains responsible? Can the
Agent receive only the tools and scopes required for one task?

Identity answers “who acted.” Accountability answers “which person owns the
outcome.” A mature design preserves both.

### 4. Execution lifecycle

Does the platform model a durable session with states such as working, waiting
for a person, failed, stopped, and finished? Can a user steer or stop it? What
happens if the process or session disappears?

An ordinary task status is insufficient because product progress and Agent
runtime state answer different questions.

### 5. Human-Agent interaction

Are progress, questions, answers, and change requests represented as typed
events or mixed into an ordinary comment stream? Can the human immediately see
what requires a decision?

The platform should expose concise semantic activity, not private model
chain-of-thought. Human prompts and decisions should remain durable and
unaltered.

### 6. Validation and delivery

What does the Agent return: generated fields, tasks, a branch, a pull request,
test output, a deployment, or merely prose? Which independent checks run, and
what human action is required before the result is accepted?

The best systems reuse normal delivery controls instead of inventing an
AI-specific bypass.

### 7. Observability and audit

Can users and administrators see current state, session history, tool actions,
artifacts, errors, responsible actors, token or credit use, and automation
triggers? Does the work-item list expose active and waiting Agent states?

Operational visibility must serve both the task owner and the administrator;
those are different views.

### 8. Cost and governance

Who enables Agents, chooses models, grants tools, sets budgets, and pays for
execution? Can an organization limit automation, users, repositories, data
sources, or destructive actions?

As Agent work becomes asynchronous and automatically triggered, cost is part
of execution policy rather than a billing footnote.

## Capability maturity model

AI integration can be understood as six cumulative levels:

| Level | Capability | Typical result |
| --- | --- | --- |
| 1 | Assistive generation | Summary, rewrite, or answer |
| 2 | Structured recommendation | Proposed fields, decomposition, routing, or criteria for human confirmation |
| 3 | Delegated participant | Agent identity, session, progress, questions, and responsibility model |
| 4 | Execution Agent | Changes external artifacts and returns a reviewable delivery result |
| 5 | Event-driven autonomy | Policy starts bounded work from triage, schedule, or system events |
| 6 | Multi-Agent orchestration | Specialized Agents coordinate through explicit contracts and shared governance |

Worktile is primarily at levels 1–2. Azure DevOps exposes much of the tool and
identity substrate required for levels 3–4 and now connects Boards items to a
GitHub Copilot execution. Linear, Jira Rovo Dev, and GitHub reach level 4;
parts of their automation models reach level 5. Public product evidence for
reliable level-6 delivery remains limited and should not drive Pactline's near
term architecture.

## Cross-competitor comparison

| Dimension | Jira / Rovo | Linear | GitHub | Azure DevOps | Worktile |
| --- | --- | --- | --- | --- | --- |
| Trigger | Chat, editor, automation, issue coding action | Delegate, mention, comment, chat, Triage automation | Issue, Agents view, PR comment, IDE, CLI, automation | Boards work item to Copilot; IDE/MCP prompt | Explicit AI button in project or task |
| Context | Teamwork Graph, Jira, Confluence, connected SaaS, repo | Issue, comments, guidance, repository, skills, MCP | Repository, issue/PR, instructions, skills, MCP, secrets/variables | Work item fields/comments plus MCP-accessible DevOps data | Current project and task information |
| Runtime identity | Generally acts for invoking user; configurable Agent profile | App/Agent user plus responsible human and delegate | Installed GitHub App / Agent with repository policy and audit | Current Entra user today; dedicated AgentId support forthcoming for DevOps MCP | Human user invokes assistive feature |
| Lifecycle | Agent/chat/automation and coding job; Jira surfaces job and PR state | First-class AgentSession and AgentActivity | First-class session logs, status, steering, stop/archive/share | Copilot status shown on work item; MCP itself is a tool surface, not a session model | Request-response generation only |
| Interaction | Chat, automation audit, Jira Agent and Development sections | Typed activities, immutable prompt, elicitation, response, external artifacts | Session log, steering, PR review comments | Work-item status and GitHub PR review; MCP client owns conversation | Edit/select/regenerate/copy/feedback |
| Delivery | Rovo Dev branch/draft PR and acceptance-criteria validation | Draft PR, diff, follow-up, merge | Branch/PR, CI, scanning, review and merge policy | Draft PR linked back to Boards; DevOps artifacts can also be manipulated via MCP | Confirmed derived tasks or advice |
| Governance | Agent creator/use controls, invoking-user permissions, restricted automation actions, credits | Admin installation/model, Agent app identity, shared AI credits | App installation, repository permissions, policies, tool allowlists, hooks, audit, credits | Entra OAuth/PAT, existing DevOps permissions, read/write tool separation, future AgentId | Existing task permissions; AI limited to flagship plan |
| Strategic strength | Organizational context and configurable orchestration | Cleanest work-management Agent protocol | Safest code-native execution and delivery loop | Enterprise identity and tool interoperability | Low-friction human-confirmed planning assistance |

## Product patterns worth adopting

### Explicit delegation, not synthetic assignment

Linear's separation between a responsible human and an Agent delegate is the
clearest model. The task does not lose its accountable owner merely because an
Agent is working. Jira's invoking-user model and GitHub's installed Agent Apps
also preserve actor attribution, but they solve different layers of identity.

Pactline should retain a responsible human and represent the active Claim as
execution delegation. An Agent should not appear as an ordinary organization
member or replace the assignee.

### Typed session activity instead of comment overload

Linear's AgentSession and AgentActivity design is the most directly relevant
reference. It distinguishes working state, a question requiring input, an
Agent response, an external artifact, and an error. It also recommends
reconstructing the durable interaction from Agent activities rather than
editable comments.

Pactline already separates Agent conversation from ordinary comments. It
should continue that direction with a small semantic activity vocabulary:

- progress summary;
- decision request;
- human answer;
- verification result;
- delivery artifact attached;
- submission;
- execution error;
- Claim release.

It should not store or request private chain-of-thought. A progress event
should say what was inspected, changed, or decided at a useful operational
level.

### Context as an inspectable snapshot

Jira's Teamwork Graph demonstrates the value of broad organizational context;
Linear's workspace/team/repository guidance demonstrates useful inheritance;
GitHub's instructions, skills, custom Agents, MCP, and tool configuration show
how code context becomes executable policy.

Pactline should not copy all source content. At Claim time it should construct
an inspectable guidance manifest containing references, versions or hashes,
precedence, and the task contract. Developer-local paths and credentials remain
local. The submission should identify which effective guidance version was
used so a later reviewer can explain drift.

### Separate tool exposure from permission

Azure DevOps' MCP server clearly separates read-only and write tools while
enforcing the authenticated user's existing permissions. Jira restricts
mutating Rovo actions in automation contexts. GitHub adds repository-scoped
tokens, tool allowlists, MCP configuration, and lifecycle hooks that can deny
or require approval for sensitive operations.

Pactline's execution contract should therefore carry both capability and
policy:

- allowed operation classes for this Claim;
- task-definition fields that execution may never mutate;
- whether external writes are allowed;
- which repository or project scopes are in bounds;
- actions requiring an explicit human decision;
- execution and cost limits.

An OpenAPI token scope alone is necessary but not sufficient once the session
can invoke arbitrary external tools.

### Ordinary delivery controls remain authoritative

GitHub's strongest design choice is that an Agent-created pull request still
passes through normal branch protection, checks, scanning, review, and merge
rules. GitHub also automatically applies code, secret, dependency, and malware
checks to third-party coding Agent pull requests. Jira Rovo Dev and Linear
similarly return draft pull requests rather than declaring the task complete.

Pactline should attach provider-neutral delivery artifacts and evidence, then
apply its own acceptance contract. “Agent finished,” “CI passed,” “PR merged,”
and “human accepted the task” must remain four distinct facts.

### Automation earns autonomy gradually

Worktile requires a person to select and confirm generated tasks. Linear keeps
automation-delegated Triage work in Triage until a person takes responsibility.
Jira restricts mutating Rovo actions when Agents are triggered by automation.
These are all versions of the same principle: autonomy should depend on input
quality, authority, reversibility, and a clear human boundary.

Pactline should begin with automatic discovery and recommendation, then allow
automatic Claim only for Todo tasks that satisfy explicit Agent-ready rules,
scope constraints, and budgets. Promotion from draft input, acceptance, merge,
deployment, and destructive external changes should remain human-controlled
until product evidence supports a narrower exception.

### Agent state belongs in collection views

Azure Boards now displays a GitHub Copilot indicator directly on board cards,
including active, completed, and failed outcomes. Linear supports delegate
filters and Agent activity reporting. GitHub provides a consolidated session
management view.

This validates Pactline's decision to show Task status and Agent status as
separate, non-editable icons in task collections. Waiting for a person should
have a subtle motion cue and link directly to the required answer; active work,
submitted work, failure, and unclaimed eligibility should be distinguishable
without opening the task.

### Usage is an execution concern

Linear and GitHub use credit-based Agent execution in addition to normal seat
pricing. Linear exposes feature/user consumption and documents that failures,
retries, and partial sessions consume actual resources, though its current
controls do not provide fine-grained per-user budgets. Jira Rovo Dev also has
separate consumption from base Rovo capabilities.

Pactline need not bill for model use, because external sessions may own that
relationship. It should nevertheless record provider-reported elapsed time,
usage, estimated cost where available, retry/release reason, and accepted
outcome. This lets a developer answer which task types are worth delegating.

## Important anti-patterns

### “AI” as one undifferentiated status

Planning assistance, active code execution, waiting for a human, validation,
and review are different states. One sparkle icon cannot communicate them.

### Editable comments as the execution ledger

Comments are useful collaboration content, but edits and mixed audiences make
them a weak source of truth for delegation, decisions, and tool outcomes.

### Invisible context expansion

An Agent that silently searches every connected data source may produce a
better answer while making authorization, confidentiality, and reproducibility
worse. Context sources and applicable guidance should be visible.

### Agent completion as product completion

This collapses execution, evidence, and acceptance. It is especially unsafe
when acceptance criteria changed after the session began.

### Transfer of a live task without transfer of context

Moving a Claim to another session implies continuity that does not exist.
Pactline's current decision is sound: bind the unfinished Claim to one session;
if that session is gone, release the Claim and return unfinished work to Todo
for a fresh start.

### Full autonomy before quality gates

Automatically executing every incoming issue magnifies poor task definition,
duplicate work, repository ambiguity, cost, and permission risk. Triage and
Agent readiness are control surfaces, not administrative overhead.

## Recommended Pactline Agent integration contract

The contract can remain provider-neutral with the following concepts. Names
are tentative; the product should extend existing Claim semantics rather than
creating parallel workflow objects.

### Execution provider

Describes the external runtime family and capabilities, not a persistent
Pactline user. Examples include Codex session, Claude Code session, GitHub
Copilot cloud Agent, or another OpenAPI client.

### Session binding

Records the opaque provider session or thread identity, generated worker ID,
Claim, start and end times, and terminal reason. The binding is immutable and
never reassigned.

### Guidance snapshot

Records the task revision, active acceptance-criterion revisions, referenced
organization/project/repository guidance, precedence, and non-secret hashes or
versions supplied at Claim time.

### Agent activity

An append-only semantic event carrying type, public summary, actor, timestamp,
optional artifact/evidence references, and whether human attention is required.
It is not a model reasoning trace.

### Decision request

A first-class activity with a stable ID, question, bounded choices or requested
input, blocking status, human answer, answering actor, and answer timestamp.
It drives the Action Center and the waiting-for-human Agent state.

### Delivery artifact

A provider-neutral reference to a branch, commit, pull request, test run, build,
deployment, release, document, or other result. The external system remains the
source of truth.

### Usage record

Optional provider-reported model, elapsed time, tokens or credits, estimated
cost, and retry information associated with a session. Missing usage is valid.

## Recommended product priorities

### P0: make the current Claim loop legible and reviewable

1. Show separate Task and Agent-state icons in List and Board.
2. Add Action Center entries for unanswered decisions and submitted Claims.
3. Standardize append-only Agent activities and decision requests.
4. Make the Claim/session/provider identity visible in the task timeline.
5. Make the submission card criterion-aware, with evidence and one-by-one
   human checks leading directly to completion when all active criteria pass.

### P1: make execution reproducible and measurable

1. Add effective guidance snapshots.
2. Add provider-neutral delivery artifacts and external event ingestion.
3. Record session duration, usage when available, release reasons, rework, and
   first-pass acceptance.
4. Add explicit Agent-ready evaluation and ranked automatic task selection.
5. Expose scoped tool/operation policy in the Claim contract.

### P2: allow bounded automation

1. Trigger Claims from approved intake rules, schedules, or monitors.
2. Add project/provider concurrency and cost budgets.
3. Add provider installations only when they improve authentication or event
   delivery; do not make one execution vendor mandatory.
4. Explore specialized Agents only after single-session execution and review
   metrics are reliable.

## Questions for product discussion

1. Which Agent activities must be durable domain events, and which operational
   logs should remain in the external session?
2. Should a Claim store an immutable guidance snapshot at acquisition, or only
   a manifest that can resolve external guidance versions later?
3. Which external operations should Pactline represent as explicit grants
   even though the external runtime ultimately enforces them?
4. Should automatic task selection require a human-approved `agent_ready`
   marker, a computed readiness result, or both?
5. What usage data can Codex and other external sessions reliably report
   without coupling the API to one provider?
6. Should non-blocking Agent questions create Action Center items while the
   Agent continues, or only blocking decision requests?
7. Which provider should validate the first DeliveryArtifact integration:
   GitHub, a manually attached link, or a generic webhook contract?

## Primary sources

### Jira and Atlassian

- [Rovo Agents](https://support.atlassian.com/rovo/docs/agents/)
- [Rovo Agent permissions and governance](https://support.atlassian.com/rovo/docs/rovo-agent-permissions-and-governance/)
- [Rovo action module](https://developer.atlassian.com/platform/forge/manifest-reference/modules/rovo-action/)
- [Rovo Dev](https://www.atlassian.com/software/rovo-dev)
- [Jira Coding Agent in automations](https://support.atlassian.com/jira-software-cloud/docs/work-with-jira-coding-agent-in-automations/)

### Linear

- [Coding Sessions](https://linear.app/docs/coding-sessions)
- [Agent interaction model](https://linear.app/developers/agent-interaction)
- [Agent developer guide](https://linear.app/developers/agents)
- [Agent best practices](https://linear.app/developers/agent-best-practices)
- [AI credits](https://linear.app/docs/ai-credits)

### GitHub

- [Third-party coding Agents](https://docs.github.com/en/copilot/concepts/agents/about-third-party-coding-agents)
- [Custom Agent configuration](https://docs.github.com/en/copilot/reference/custom-agents-configuration)
- [Managing Agent sessions](https://docs.github.com/en/copilot/how-tos/copilot-on-github/use-copilot-agents/manage-and-track-agents)
- [Agent management](https://docs.github.com/en/copilot/concepts/agents/cloud-agent/agent-management)
- [Agent hooks](https://docs.github.com/en/copilot/concepts/agents/hooks)

### Azure DevOps

- [Remote Azure DevOps MCP Server](https://learn.microsoft.com/en-us/azure/devops/mcp-server/remote-mcp-server?view=azure-devops)
- [Use GitHub Copilot with Azure Boards](https://learn.microsoft.com/en-us/azure/devops/boards/github/work-item-integration-github-copilot?view=azure-devops)
- [Microsoft Entra Agent identities](https://learn.microsoft.com/en-us/entra/agent-id/what-are-agent-identities)
- [Authorization for Agent identities](https://learn.microsoft.com/en-us/entra/agent-id/authorization-agent-id)

### Worktile

- [Worktile AI](https://worktile.com/ai)
- [Worktile 9.52.0 AI release](https://worktile.com/blog/v-9-52-0-release/)
- [Worktile service terms](https://worktile.com/terms)
