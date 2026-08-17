# Pactline Fleet Context

Pactline Fleet coordinates Pactline work through interchangeable Agent
Harnesses while keeping workflow and repository authority outside the
Harness. This glossary defines the language of that coordination context.

## Coordination

**Fleet Service**:
One resident coordinator connected to one Pactline instance and responsible
for one local scheduling and recovery boundary.
_Avoid_: Fleet daemon, global Fleet

**Fleet**:
A local scheduling and policy definition for exactly one Pactline Project.
It is not a Project status and is not registered globally in Pactline.
_Avoid_: worker, Project Fleet record

**Run**:
One locally identified attempt to coordinate one Task through one Claim stage
under a frozen policy.
_Avoid_: Task, Claim, job

**Work Candidate**:
A bounded view of Pactline work that is currently eligible for one Fleet stage
but has not yet been admitted as a Run.
_Avoid_: queued Run, claimed Task

**Work Definition**:
The immutable repository, revision, path, verification, criterion, and delivery
policy required to admit a Work Candidate.
_Avoid_: Task prompt, Harness configuration

**Run Stage**:
The Fleet purpose of a Run: execution, review, or correction. Resolution
analysis is a Harness activity inside workflow coordination, not a separately
scheduled Run Stage.
_Avoid_: Task phase, Harness mode

## Harness boundary

**Harness Adapter**:
The translation boundary between Fleet's common Run contract and one Agent
Harness's native Session, tools, events, and terminal result.
_Avoid_: Fleet plugin, workflow engine

**Adapter Session**:
The Harness-native execution identity associated with a Run, such as a Codex
Thread or DeepSeek Session.
_Avoid_: Run, Claim, client session

**Harness Proposal**:
A structured recommendation returned by a Harness for Fleet to validate. It
has no workflow or repository authority on its own.
_Avoid_: settlement, delivery result

## Authority and recovery

**Work Plugin**:
A trusted Fleet extension that resolves repository policy and performs
provider-specific delivery operations without granting that authority to a
Harness.
_Avoid_: Harness plugin, Agent tool

**External Effect**:
A potentially non-local operation whose intent and observed result are
recorded so Fleet can reconcile uncertainty without blindly repeating it.
_Avoid_: event, log entry

**Checkpoint**:
The latest durable coordination boundary reached by a Run.
_Avoid_: Task status, Pactline phase

**Quarantine**:
A terminal local Run disposition used when Fleet cannot safely prove whether
an external effect or authority transition occurred.
_Avoid_: failure, cancellation

**Settlement**:
The Pactline workflow mutation that concludes the active Claim outcome for a
Run after Fleet-owned validation and delivery work.
_Avoid_: Harness completion, Git push

**Observation**:
A bounded read-only projection of authoritative Pactline facts and local Fleet
coordination facts for operators.
_Avoid_: source of truth, control plane
