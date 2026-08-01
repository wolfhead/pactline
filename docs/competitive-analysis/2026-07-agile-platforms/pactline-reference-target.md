# Pactline Competitive Reference Target

Research date: 2026-07-31

## Executive conclusion

Pactline should not pursue feature parity with Jira, Linear, GitHub Projects,
Azure Boards, or Worktile. It should become the clearest and safest coordination
system for software work shared by people and independently running AI coding
sessions.

The recommended position is:

> Pactline is an Agent-native delivery workspace where humans define outcomes
> and acceptance, real coding sessions execute work in developer-controlled
> environments, and every decision, artifact, verification result, and handoff
> remains inspectable.

The market has moved quickly. Linear and GitHub can already delegate Issues to
Codex or other coding Agents and return pull requests for human review. Jira is
embedding Rovo Agents across planning and organizational knowledge. Azure
DevOps exposes work and delivery data through MCP. Therefore, "AI can take a
task and write code" is table stakes.

Pactline's opportunity is a stronger execution contract, not merely an Agent
button.

## Research scope

This target synthesizes the accompanying analyses of:

- [Cross-platform AI integration patterns](ai-integration-patterns.md)
- [Jira](jira.md)
- [Linear](linear.md)
- [GitHub Projects](github-projects.md)
- [Azure Boards](azure-boards.md)
- [Worktile](worktile.md)

The recommendations are evaluated against Pactline's current product model:
long-lived Projects, delivery Milestones, Tasks with explicit Context and
Expected Result, revisioned Acceptance Criteria, immutable Checks, separate
human and Agent conversations, session-bound Claims, human review, and a
contract-first API.

## Competitive landscape

### Mature agile planning

Jira and Azure Boards establish the baseline for ranked backlogs, iterations,
capacity, dependencies, cross-team planning, dashboards, permissions, and flow
analytics. Their weakness is configuration and administration cost.

### Opinionated product execution

Linear demonstrates that a strong domain model and fast interaction can replace
much configuration. Triage, Backlog, Cycle, Project, and Initiative answer
different planning questions without becoming one generic work-item graph.

### Code-native coordination

GitHub demonstrates the value of keeping Issues, pull requests, reviews, CI,
security, and deployment close together. Its planning model is flexible but
less explicit about intake quality and acceptance.

### Chinese enterprise collaboration

Worktile demonstrates the importance of familiar views, templates, approvals,
Lark/WeCom-style integration, private deployment, and implementation support.
Its broad office-collaboration surface is not a suitable product boundary for
Pactline.

### Agent-native competition

Linear Coding Sessions and GitHub Copilot cloud agent now cover:

- assignment or delegation from an issue;
- hosted asynchronous execution;
- issue and repository context;
- progress visibility and steering;
- draft pull requests and diffs;
- human review and change requests;
- CI repair and merge workflows;
- usage-based AI billing.

Pactline must assume these capabilities will become common across development
platforms.

The implementations are not interchangeable. Linear has the strongest
work-management session and activity model; GitHub has the strongest code,
tool-policy, security-check, and pull-request boundary; Jira has the broadest
organizational context and configurable Agent/automation surface; Azure is
building the enterprise MCP and Agent-identity substrate; Worktile has the
clearest low-risk proposal-and-confirm interaction. Pactline should combine
these patterns around its existing acceptance contract rather than copying one
vendor's runtime.

## Pactline's existing strategic assets

Pactline already has several strong foundations that should not be diluted.

### Explicit outcome contract

Task creation requires Context and Expected Result. This is better for Agent
execution than a title-only issue or a generic task description.

### Revisioned acceptance

Acceptance Criteria are revisioned and Checks are immutable. An Agent's
self-verification is evidence, while only a fresh human pass or waiver after
Agent submission satisfies completion. This is stronger than treating CI green,
PR merged, or Agent finished as synonymous with accepted.

### Separate execution state

Task status and Agent status are separate. A Task can remain a product object
while its Claim records whether an Agent is active, waiting for a person, or
submitted for review.

### Real-session binding

A Claim binds one unfinished Task to one real Codex session. Claims are not
transferred across sessions, avoiding false continuity after execution context
has disappeared.

### Separate conversation with a unified timeline

Agent progress, questions, answers, and submissions are not mixed into ordinary
comments, but humans can still understand events chronologically.

### Open execution contract

External Agents use a scoped OpenAPI contract and a reusable Codex skill rather
than requiring all execution to happen in a platform-owned cloud sandbox.

### Self-hostable coordination

Pactline can be deployed by the organization controlling its tasks and Agent
credentials. This matters where source code or execution environments cannot be
connected to a third-party managed Agent runtime.

## Proposed target users

The initial target should remain narrow:

1. individual developers maintaining several repositories and using multiple
   Codex sessions;
2. small software teams delegating bounded tasks to coding Agents while keeping
   a human owner;
3. teams needing stronger acceptance evidence and local-environment access than
   hosted coding Agents provide;
4. organizations that need a self-hosted coordination layer without deploying
   a full enterprise DevOps suite.

Pactline should not initially optimize for portfolio-management offices,
nontechnical departments, large-scale HR utilization reporting, or arbitrary
business-process automation.

## Jobs to be done

### Human developer

> Show me what requires my judgment, what an Agent can safely do next, what is
> currently running, and exactly what I must inspect before I accept it.

### Engineering lead

> Let me understand delivery risk, review load, Agent effectiveness, and blocked
> work without reading every task or Agent transcript.

### Coding Agent session

> Give me one eligible task with complete context, repository guidance, bounded
> authority, acceptance criteria, and an unambiguous way to report progress,
> request a decision, verify work, and submit evidence.

### Reviewer

> Present the outcome, changed artifacts, test evidence, known limitations, and
> acceptance criteria together so I can review quickly without trusting the
> Agent's conclusion.

## Product principles

### Human accountability remains explicit

An Agent is a delegate, not the responsible owner. Every Agent-executed Task
should retain one responsible human. Agent work must remain visible in that
person's work queue.

### One task, one current execution owner

Only one unfinished Claim may exist for a Task, and one real session may hold at
most one unfinished Claim. No background transfer should imply preserved
context when none exists.

### Evidence is not acceptance

CI output, test results, screenshots, PR links, and Agent summaries are evidence.
Completion remains an explicit product decision subject to active criteria.

### Stable semantics before configurability

Agents require predictable contracts. Task, Claim, status, acceptance,
conversation, and provenance semantics should remain stable even if views,
templates, and filters become customizable.

### External systems remain sources of truth

Pactline should reference code, PRs, CI, deployments, documents, and incidents,
not clone their full data models.

### Attention is the scarce human resource

The primary human UI should optimize questions, reviews, decisions, and risk,
not merely display all tasks.

### Authority is explicit and trigger-sensitive

The same Agent should not automatically receive the same authority in an
interactive request and an unattended monitor. Every Claim should expose the
allowed Pactline operations, task/repository scope, and actions that require a
human decision. Automatic starts should use narrower policy than direct human
delegation.

### Semantic progress, not hidden reasoning

Pactline should store operationally useful Agent activities—progress,
questions, actions, checks, artifacts, errors, and submissions—not private
model chain-of-thought. Human prompts, answers, and acceptance decisions should
be append-only and attributable.

### Effective context is inspectable

At Claim time, the task contract and applicable guidance should form an
inspectable manifest. A reviewer must be able to see which task and acceptance
revisions, Project guidance, and repository guidance the session used without
exposing credentials or developer-local paths.

## Reference capability model

### Layer 1: intake and readiness

```text
Inbox item or Draft
  -> human or AI enrichment
  -> Context and Expected Result complete
  -> Acceptance Criteria reviewed
  -> Project, responsible human, and execution mode selected
  -> formal Task eligible for planning
```

This layer prevents low-quality or automated input from entering the executable
Task pool. AI may suggest decomposition, fields, criteria, or Agent suitability,
but a person confirms promotion.

### Layer 2: planning

```text
Project Backlog
  -> relative rank
  -> optional Cycle
  -> Milestone / delivery outcome
  -> dependencies and one-level decomposition
  -> Ready for human or Agent execution
```

Priority remains a category. Rank establishes actual ordering. Cycle provides
recurring cadence; Milestone remains an outcome-oriented delivery window with
its own acceptance.

### Layer 3: execution

```text
Eligible Task
  -> session-bound Claim
  -> active execution
  -> progress evidence
  -> optional waiting_human question
  -> same session resumes after an answer
  -> verification
  -> submission or release
```

Execution can occur through an external Codex session, a future built-in Agent,
or another provider implementing the same contract. Provider-specific details
should be metadata, not core state semantics.

### Layer 4: review and acceptance

```text
Agent submission
  -> outcome summary
  -> development artifacts
  -> criterion-specific self-check evidence
  -> human pass, fail, or waiver
  -> change request or explicit Task completion
```

Review should be possible from the submission card. A reviewer should not need
to reconstruct the work from the complete timeline.

### Layer 5: learning and improvement

```text
Task and Claim history
  -> flow metrics
  -> Agent effectiveness
  -> recurring blockers
  -> template and guidance improvements
  -> better readiness and planning
```

Metrics should improve the system, not rank individuals.

## Reference Agent integration contract

This is a conceptual contract, not a commitment to new tables with these exact
names. Existing Claim, conversation, acceptance, and timeline concepts should
be extended where they already carry the correct ownership.

### Execution provider and session binding

The provider describes the runtime family and reported capabilities, such as a
Codex session, Claude Code session, or GitHub Copilot cloud Agent. It is not an
ordinary Pactline user.

The session binding records:

- generated worker identity and opaque external session/thread identity;
- provider and execution-environment metadata;
- Claim, responsible human, start/end timestamps, and terminal reason;
- immutable provenance that cannot be reassigned to another session.

If the session disappears, the Claim is released and unfinished work returns
to Todo. A new session starts a new Claim and reconstructs context from the
Task; it does not claim continuity with the old session.

### Guidance snapshot

The effective context manifest should record:

- Task revision and active Acceptance Criterion revisions;
- organization, Project, repository, and Task guidance references;
- precedence and non-secret version or hash information;
- selected repositories and declared execution scope;
- permitted operation classes and human-approval boundaries.

Developer-specific repository paths and credentials remain in local Pactline
configuration and are never copied into shared Task data.

### Semantic Agent activity

Use a small append-only vocabulary:

- progress summary;
- decision request;
- human answer;
- verification result;
- delivery artifact attached;
- execution error;
- submission;
- Claim release.

An activity includes actor, timestamp, public summary, optional evidence or
artifact references, and whether human attention is required. Ordinary
comments remain a separate collaboration channel; both appear in the unified
timeline.

### Decision request

A decision request needs a stable identity, question, blocking/non-blocking
status, optional bounded choices, answering human, answer, and timestamps. A
blocking open request drives `waiting_human` and the Action Center. The same
session resumes after the answer.

### Delivery artifact and verification evidence

Branches, commits, pull requests, CI runs, deployments, test runs, and documents
are provider-neutral references. External systems remain authoritative. Each
artifact or check can be associated with a Claim submission and a specific
criterion revision.

The product must keep these facts separate:

1. the external Agent stopped running;
2. its verification checks passed;
3. a pull request merged or another artifact published;
4. a human accepted every active criterion;
5. the Task entered Done.

### Usage and execution policy

Where available, record provider-reported model, elapsed time, tokens or
credits, estimated cost, retries, and release reason. Missing provider usage is
valid. Aggregate measures should answer which task patterns produce accepted
outcomes efficiently, not score individual developers.

Before event-driven automatic Claiming, add Project/provider concurrency and
cost limits. Monitoring without an execution budget is observability, not
governance.

## Recommended capabilities

### P0: next product foundation

#### 1. Action Center

Create a first-class, clearable queue with sections such as:

- Agent waiting for my answer;
- Agent submission awaiting my review;
- requested changes returned by me;
- tasks where I am responsible and risk is increasing;
- mentions and explicit decisions;
- overdue acceptance or stale work.

This should be separate from the full notification stream. Each item must expose
the action that clears it: answer, review, decide, acknowledge, or replan.

Decision requests should be typed Agent activities rather than ordinary
comments. A blocking unanswered request is generated from domain state, and a
human answer remains immutable in the Agent conversation.

Reference: Linear Inbox and My Issues, GitHub Copilot My Work, Worktile approval
entry points.

#### 2. Ranked Backlog

Add relative `rank` independently of `priority`.

Required behavior:

- manual drag ordering;
- stable ordering under concurrent edits;
- rank scoped at least to Project Backlog and filtered planning collections;
- visible Agent eligibility and dependency warnings;
- Agent task selection uses rank as one explicit input, not a hidden heuristic;
- audit important reorder operations without flooding the timeline.

Reference: Jira and Azure Boards ranked backlogs.

#### 3. Board and Saved Views

Add Board as another renderer of the filtered Task collection. Allow grouping
by Task status, responsible human, Agent status, Milestone, or Cycle.

Add SavedView with:

- filters;
- grouping;
- ordering;
- visible fields;
- layout type;
- personal or shared visibility;
- favorite/default behavior.

Do not allow Board columns to create a second hidden state system.

Reference: Linear Custom Views, GitHub Projects, Jira saved filters.

#### 4. Development links and delivery evidence

Introduce a provider-neutral model, tentatively `DevelopmentLink` or
`DeliveryArtifact`:

- provider and repository identity;
- artifact type: branch, commit, pull request, build, deployment, release,
  incident, or test run;
- external ID and URL;
- observed state and timestamps;
- actor and ingestion provenance;
- optional relation to a Claim submission or Acceptance Check.

Render a compact delivery summary on the Task and submission. External systems
remain authoritative. A merged PR or successful build never completes the Task
by itself.

Reference: GitHub and Azure DevOps traceability, Linear GitHub integration.

The first implementation should also expose provider, Claim, and session
provenance on each submission. GitHub's normal CI and security controls remain
evidence sources; they do not become Pactline acceptance authorities.

#### 5. Flow metrics

Provide built-in measures before a generic dashboard builder:

- backlog age;
- time to Claim;
- Agent active time;
- waiting-for-human time;
- human response time;
- submission-to-review time;
- first-pass acceptance rate;
- rework loops;
- Claim release and expiry rate by reason;
- total lead and cycle time;
- throughput by Project and Milestone;
- review queue size and age.

Use medians and percentiles, not only averages. Exclude or separately show
canceled and released work where appropriate.

Reference: Jira control charts, Linear Insights, Azure Analytics.

### P1: planning and repeatability

#### 6. Optional Cycle

Add Cycle as a recurring planning window separate from Milestone.

Potential scope:

- one-to-four-week cadence initially;
- current and future Cycles generated automatically;
- manual selection of rollover policy;
- capacity hint based on recent throughput;
- optional cooldown or unplanned-work allowance;
- Cycle history preserved as a planning snapshot.

Do not make every Project use Cycles.

Reference: Linear Cycles, Jira Sprints, Azure Iterations.

#### 7. Intake / Triage

Create a lightweight InboxItem or TaskDraft for input from Lark, users, Agents,
monitoring systems, and integrations.

Promotion to Task should require the same current Task contract. Drafts are not
Claimable and do not appear as unfinished delivery work.

AI can suggest:

- duplicates and related work;
- Project, responsible human, labels, and priority;
- Context and Expected Result improvements;
- acceptance criteria;
- human-only versus Agent-allowed execution.

Reference: Linear Triage and Triage Intelligence.

#### 8. Task and acceptance templates

A template should encode:

- Context structure;
- Expected Result structure;
- default Acceptance Criteria;
- execution mode;
- default labels and Project/Milestone hints;
- repository or environment guidance references;
- recommended verification commands;
- constraints and known non-goals.

Templates should improve Agent readiness rather than become a generic form
builder.

Reference: Worktile project templates, GitHub Project templates, Linear issue
templates.

#### 9. Guidance inheritance

Make Agent guidance discoverable and layered:

```text
organization guidance
  -> Project guidance
    -> repository guidance
      -> Task-specific instructions
```

The effective guidance supplied to a Claim should be inspectable. Sensitive
developer-local mapping stays in the developer's environment and is never
copied into the public Project record.

Persist a non-secret guidance manifest or snapshot reference with the Claim so
later edits do not make past behavior inexplicable.

Reference: Linear Agent guidance and repository skills, Jira Teamwork Graph.

#### 10. Milestone health updates

Allow the Milestone owner to post a structured update:

- On track, At risk, or Off track;
- what changed;
- current blockers;
- decisions required;
- next expected outcome.

Automate draft summaries and reminders, but require a person to publish the
health assessment.

Reference: Linear Project and Initiative updates.

### P2: scale only after evidence

Potential later capabilities include:

- cross-Project initiatives or objectives;
- cross-team delivery plans;
- bounded workflow automation;
- additional identity providers and organization roles;
- cost and concurrency budgets for Agent execution;
- provider-specific Agent installations;
- richer manual test management;
- organization-wide analytics exports.

Each requires demonstrated use. None should block the P0 Agent execution and
human attention loop.

## Explicit non-goals

Pactline should not become:

- a generic office suite with chat, drive, calendar, and OKRs;
- a generic custom-field database;
- an arbitrary workflow engine;
- a source-code host or CI service;
- an individual productivity-scoring system;
- a mandatory Scrum process;
- a deep portfolio hierarchy before cross-team demand exists;
- a platform that lets Agent or automation evidence silently become human
  acceptance.

## UX reference target

### Navigation

The primary information architecture should answer four questions:

1. What needs my attention?
2. What work is planned and in what order?
3. What are humans and Agents doing now?
4. What is ready for review or at risk?

Suggested top-level surfaces:

```text
My work
  - Needs attention
  - Assigned to me
  - Delegated by me
Projects
  - Backlog
  - Board
  - Gantt / timeline
  - Saved views
Reviews
  - Agent submissions
  - Acceptance history
Insights
  - Flow
  - Agent collaboration
```

### Task list and board

- Task status and Agent status remain distinct icons.
- Responsible human remains visible for delegated work.
- Waiting-for-human and submitted states are actionable, not decorative.
- Rank, dependency, due risk, and acceptance readiness are visible without
  opening the Task.

### Task detail

The detail surface should prioritize:

1. Context and Expected Result;
2. current acceptance criteria;
3. current responsible human and Agent state;
4. development artifacts and verification;
5. latest question or submission action;
6. unified chronological timeline.

The full transcript remains available but should not dominate the review
decision.

### Agent submission

The submission card should summarize:

- outcome and changed scope;
- repositories and pull requests;
- checks executed and results;
- criterion-specific evidence;
- known limitations and follow-up work;
- Agent identity, provider, Claim, and session provenance;
- actions: inspect, pass, fail, waive, request changes, or complete.

## Competitive response matrix

| Competitor capability | Pactline response |
| --- | --- |
| Jira configurable workflow | Preserve fixed core semantics; add bounded automation later |
| Jira/Azure ranked backlog | Add explicit relative rank |
| Jira/Linear flow analytics | Add Agent-aware lead, execution, waiting, and review metrics |
| Linear Triage | Add non-Claimable InboxItem or TaskDraft |
| Linear Cycles | Add optional Cycle separate from Milestone |
| Linear human + Agent delegation | Preserve human assignee and show separate Agent delegate/Claim |
| Linear AgentSession and typed activities | Use session-bound Claims and append-only semantic Agent activities |
| Linear Coding Sessions | Differentiate through external real sessions, local environments, and acceptance evidence |
| GitHub Issue-to-PR traceability | Add provider-neutral DevelopmentLink and event ingestion |
| GitHub first/third-party coding Agents | Support provider-neutral skills and self-hosted coordination, with local execution as a first-class environment |
| GitHub tools, hooks, and security checks | Add explicit Claim operation policy and ingest checks as evidence |
| Jira Teamwork Graph and Rovo actions | Add inspectable guidance inheritance and trigger-sensitive authority, not a general knowledge graph |
| Azure MCP read/write tools and Agent ID direction | Keep scoped OpenAPI/MCP access and explicit Agent/session identity |
| Azure Test Plans | Link external test runs to revisioned Acceptance Checks |
| Worktile AI decomposition | Keep AI-generated structure as an editable proposal until human confirmation |
| Worktile inline approvals | Keep criterion-by-criterion review in the Agent submission card |
| Worktile templates | Build development and Agent-readiness templates, not arbitrary business forms |
| Worktile private deployment | Keep deployment, upgrades, backups, and credential handling first-class |

## Measures of product success

Do not measure success primarily by number of Tasks or Agent invocations.

Recommended product measures:

- percentage of eligible tasks completed with accepted evidence;
- median human attention time per accepted Agent task;
- first-pass acceptance rate;
- median waiting-for-human and review delay;
- Claim release or expiry rate;
- percentage of submissions with linked development artifacts and checks;
- backlog age and throughput predictability;
- number of Agent-ready Tasks that required clarification after Claim;
- proportion of human decisions handled through the Action Center;
- repeat usage by developers across multiple Projects or repositories.

Guardrail measures:

- human acceptance overridden or bypassed: must remain zero;
- task definition modified by an execution-scoped Agent: must remain zero;
- Claim resumed by a different session: must remain zero;
- secrets or developer-local paths exposed through task artifacts: must remain
  zero;
- completion reverted because evidence was misleading or stale;
- Agent execution cost without an accepted outcome.

## Recommended development sequence

### Phase A: attention and planning

1. Action Center.
2. Ranked Backlog.
3. Board and Saved Views.

This phase makes the existing Agent workflow usable at growing task volume.

### Phase B: delivery evidence and learning

4. DevelopmentLink and CI/PR integration.
5. Agent-aware flow analytics.
6. richer submission and change-request loop.

This phase strengthens Pactline's acceptance moat.

### Phase C: repeatable planning

7. TaskDraft/Triage.
8. Task and acceptance templates.
9. optional Cycle.
10. guidance inheritance and inspectability.

This phase increases the supply of high-quality Agent-ready work.

### Phase D: organizational scale

11. Milestone health updates.
12. cross-Project planning only when real demand exists.
13. bounded automation and more granular governance.

## Decisions for the next product discussion

The following choices materially affect architecture and should be discussed
before implementation:

1. Is Pactline's first target a single developer with many Codex sessions, or a
   small team sharing Agent capacity?
2. Should responsible human ownership remain the current Task assignee, or be a
   separate delegation owner field?
3. Is relative rank global within a Project Backlog, or specific to each
   planning context such as Milestone and Cycle?
4. Should the Action Center be derived entirely from current domain state, or
   should it store durable inbox items with snooze and acknowledgement?
5. Which source provider should validate the first DevelopmentLink design:
   GitHub, GitLab, or a provider-neutral manually attached link?
6. Should a requested-change review reactivate the same completed Claim context
   where technically available, or always return the Task to Todo for a new
   Claim?
7. Is Cycle valuable for the initial developer persona, or should throughput
   and ranked backlog come first without iteration ceremony?
8. Which Agent execution costs, token usage, or elapsed-time data should become
   visible to the human reviewer?
9. Which Agent activities belong in the durable domain history, and which
   detailed operational logs should remain only in the external session?
10. Should Agent readiness be a human-approved marker, a computed assessment,
    or both?
11. Which Pactline and external operations require explicit per-Claim grants
    beyond token scopes?

## Primary external references

- [Linear Coding Sessions](https://linear.app/docs/coding-sessions)
- [Cross-platform AI integration patterns](ai-integration-patterns.md)
- [Linear AI Agents](https://linear.app/docs/agents-in-linear)
- [GitHub Copilot cloud-agent workflow](https://docs.github.com/en/copilot/tutorials/cloud-agent/improve-a-project)
- [Jira Rovo and Agent direction](https://www.atlassian.com/software/jira/ai)
- [Azure DevOps remote MCP Server](https://learn.microsoft.com/en-us/azure/devops/mcp-server/remote-mcp-server?view=azure-devops)
- [Worktile pricing and capability matrix](https://worktile.com/pricing)
