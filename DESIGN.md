---
name: Pactline
description: A modern, low-noise workbench for human-Agent project delivery.
colors:
  glacier-signal-blue: "#2563eb"
  glacier-signal-wash: "#e6efff"
  confluence-teal: "#0f766e"
  confluence-wash: "#e2f4f1"
  glacier-canvas: "#f3f7fb"
  sidebar-mist: "#eaf2f9"
  white-surface: "#ffffff"
  quiet-surface: "#eef4f8"
  deep-slate-ink: "#172b3d"
  slate-muted: "#4f6274"
  slate-subtle: "#687a8b"
  soft-divider: "#d8e3ec"
  strong-control-stroke: "#718196"
  danger-red: "#b4232c"
  danger-wash: "#fcebec"
  progress-amber: "#b45309"
  review-ochre: "#8a5a05"
  high-priority-orange: "#c2410c"
typography:
  headline:
    fontFamily: "-apple-system, system-ui, Segoe UI, Roboto, Helvetica Neue, Noto Sans, Arial, sans-serif"
    fontSize: "20px"
    fontWeight: 600
    lineHeight: "28px"
  section:
    fontFamily: "-apple-system, system-ui, Segoe UI, Roboto, Helvetica Neue, Noto Sans, Arial, sans-serif"
    fontSize: "18px"
    fontWeight: 600
    lineHeight: "28px"
  title:
    fontFamily: "-apple-system, system-ui, Segoe UI, Roboto, Helvetica Neue, Noto Sans, Arial, sans-serif"
    fontSize: "16px"
    fontWeight: 600
    lineHeight: "24px"
  body:
    fontFamily: "-apple-system, system-ui, Segoe UI, Roboto, Helvetica Neue, Noto Sans, Arial, sans-serif"
    fontSize: "14px"
    fontWeight: 400
    lineHeight: "20px"
  label:
    fontFamily: "-apple-system, system-ui, Segoe UI, Roboto, Helvetica Neue, Noto Sans, Arial, sans-serif"
    fontSize: "12px"
    fontWeight: 500
    lineHeight: "16px"
  mono-label:
    fontFamily: "ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, Liberation Mono, monospace"
    fontSize: "12px"
    fontWeight: 400
    lineHeight: "16px"
rounded:
  compact: "4px"
  control: "6px"
  surface: "8px"
  prominent-surface: "12px"
  pill: "9999px"
spacing:
  hairline: "4px"
  compact: "8px"
  control: "12px"
  surface: "16px"
  section: "20px"
  page: "24px"
components:
  button-primary:
    backgroundColor: "{colors.glacier-signal-blue}"
    textColor: "{colors.white-surface}"
    typography: "{typography.body}"
    rounded: "{rounded.control}"
    padding: "8px 12px"
    height: "40px"
  button-quiet:
    backgroundColor: "{colors.white-surface}"
    textColor: "{colors.deep-slate-ink}"
    typography: "{typography.body}"
    rounded: "{rounded.control}"
    padding: "6px 12px"
  input-compact:
    backgroundColor: "{colors.white-surface}"
    textColor: "{colors.deep-slate-ink}"
    typography: "{typography.body}"
    rounded: "{rounded.control}"
    padding: "0 10px"
    height: "32px"
  navigation-active:
    backgroundColor: "{colors.glacier-signal-blue}"
    textColor: "{colors.white-surface}"
    typography: "{typography.body}"
    rounded: "{rounded.control}"
    padding: "8px 12px"
  metadata-chip:
    backgroundColor: "{colors.quiet-surface}"
    textColor: "{colors.slate-muted}"
    typography: "{typography.label}"
    rounded: "{rounded.pill}"
    padding: "2px 8px"
  surface-card:
    backgroundColor: "{colors.white-surface}"
    textColor: "{colors.deep-slate-ink}"
    rounded: "{rounded.surface}"
    padding: "16px"
  task-row:
    backgroundColor: "{colors.white-surface}"
    textColor: "{colors.deep-slate-ink}"
    typography: "{typography.body}"
    padding: "0 12px"
    height: "40px"
---

# Design System: Pactline

## Overview

**Creative North Star: "The Converging Workbench"**

Pactline is a modern collaborative workbench where human and Agent workstreams
converge into one legible delivery path. It should feel direct, precise, and
current without becoming flashy. Fast inline actions, immediate state feedback,
and clear focus make it modern; ornamental effects do not.

The interface is a working surface rather than an electronic ledger. Audit and
structure are trustworthy foundations, but they stay beneath a calm operating
experience. Information hierarchy follows the user's next decision, so the
most actionable risk, task, or outcome receives emphasis before supporting
metadata.

The system evolves through focused improvements rather than a one-time visual
replacement. New work should strengthen the shared hierarchy and interaction
grammar while preserving familiar workflows.

**Key Characteristics:**

- Modern and direct, without novelty for its own sake
- Dense enough for daily operation, but never visually flat
- Quiet at rest and unmistakable during action
- Structured by user attention rather than database shape
- Layered through tone, spacing, and borders before shadow

## Colors

The palette combines a cool, low-noise workspace with two purposeful signals:
Glacier Signal Blue directs action and selection, while Confluence Teal marks
completion and constructive secondary emphasis.

### Primary

- **Glacier Signal Blue:** Reserved for primary actions, selected navigation,
  focus, and the strongest current-state indicator.
- **Glacier Signal Wash:** Carries selection and focus context without turning
  a whole region into a solid blue block.

### Secondary

- **Confluence Teal:** Marks completed work and restrained secondary emphasis.
- **Confluence Wash:** Supports positive or collaborative context when a solid
  teal signal would be too strong.

### Tertiary

- **Progress Amber:** Identifies active work that needs temporal attention.
- **Review Ochre:** Distinguishes review state from both progress and
  completion.
- **High-Priority Orange:** Raises priority without borrowing the destructive
  meaning of Danger Red.

### Neutral

- **Glacier Canvas:** The application ground that separates the shell from
  white work surfaces.
- **Sidebar Mist:** Gives permanent navigation its own stable region.
- **White Surface:** The main work surface, raised surface, and field
  background.
- **Quiet Surface:** Supports hover, low-emphasis grouping, and metadata.
- **Deep Slate Ink:** Anchors primary text, icons, and the brand mark.
- **Slate Muted:** Supports secondary text that remains comfortably readable.
- **Slate Subtle:** Is limited to tertiary metadata on plain light surfaces.
- **Soft Divider:** Separates rows and regions without framing every item.
- **Strong Control Stroke:** Makes persistent form fields and overlay
  boundaries discoverable.
- **Danger Red / Danger Wash:** Are reserved for destructive, invalid, or
  failed states.

### Named Rules

**The Accent Scarcity Rule.** Glacier Signal Blue identifies the current action
or location; it must not become general decoration.

**The State Has Meaning Rule.** Status, priority, danger, and selection colors
must keep stable semantics across List and Gantt renderers.

## Typography

**Display Font:** System sans-serif stack

**Body Font:** System sans-serif stack

**Label/Mono Font:** System monospace stack for stable identifiers only

**Character:** Native system typography keeps the application fast, familiar,
and operational. Hierarchy comes from a compact range of size, weight, and
color rather than oversized headings.

### Hierarchy

- **Headline:** Page and project names; the strongest heading used inside the
  application shell.
- **Section:** Major project regions such as attention and active milestones.
- **Title:** Panel, card, and meaningful subsection titles.
- **Body:** Task titles, descriptions, field values, actions, and most
  operating copy.
- **Label:** Metadata, compact controls, counts, and supporting captions.
- **Mono Label:** Task numbers, request identifiers, token prefixes, and other
  values whose stable character width aids scanning.

### Named Rules

**The Content Earns Emphasis Rule.** Weight and size follow workflow importance,
not entity rank; a blocking condition may outrank the project name when it is
the user's next decision.

## Layout

Pactline uses an eight-pixel primary rhythm with four-pixel adjustments for
compact controls. General pages use bounded containers and generous outer
padding, while task collections use the full available work surface for
efficient scanning.

Desktop task work uses three coordinated regions: permanent navigation, the
task collection, and an optional detail panel. The navigation column is stable
and the detail panel appears only when a task is selected. Project pages use a
bounded content column, clear section gaps, and grids that collapse before
their contents become cramped.

Responsive behavior changes the arrangement, not the product model. Phones use
a compact task card, bottom navigation, and full-page or sheet-based secondary
work. Medium widths move navigation into a drawer. Permanent navigation begins
at the desktop breakpoint, and wider screens progressively expose secondary
task properties.

**The Attention Follows Work Rule.** Visual priority must match the user's
likely attention order:

- My Work leads with overdue work, blockers, and the next actionable task.
- Project views lead with delivery risk and active Milestones.
- Milestone views lead with unfinished work, dependency blockers, and
  acceptance state.
- Task detail leads with context and expected result before history and
  tertiary metadata.
- Backlog leads with prioritization and scheduling decisions.

**The Shared Collection Rule.** List and Gantt are renderers of the same task
collection. Filters, selection, loading, errors, pagination, and mutations must
keep the same placement and behavior unless the renderer requires a genuinely
different interaction.

## Elevation & Depth

The system is layered, not carded. Canvas tone, white work surfaces, spacing,
and hairline dividers provide normal structure. Shadows are ambient separators
for a focused panel, floating overlay, or interactive lift; they are not
permanent decoration on every container.

### Shadow Vocabulary

- **Shell Hairline** (`0 1px 3px rgb(23 43 61 / 0.04)`): Separates the fixed
  application header from the work surface.
- **Control Lift** (`0 1px 3px rgb(0 0 0 / 0.10), 0 1px 2px -1px rgb(0 0 0 / 0.10)`):
  Gives the primary navigation action and active navigation item restrained
  tactile emphasis.
- **Detail Separation** (`-8px 0 24px rgb(23 43 61 / 0.04)`): Distinguishes an
  open detail panel without making it feel like a modal.

### Named Rules

**The Layered, Not Carded Rule.** Add a card only when content is independently
selectable, movable, or conceptually bounded; use spacing and dividers for
ordinary grouping.

## Shapes

Controls use gently rounded corners, compact inline editors use the tightest
radius, normal cards use a medium radius, and prominent shell surfaces may use
the largest radius. Pills are reserved for tags, avatars, and small status-like
metadata whose silhouette carries meaning.

Borders are usually one pixel. Transparent borders preserve control geometry
when an inline property is quiet at rest, while a strong stroke is reserved for
persistent fields and overlay boundaries.

**The Radius Has a Job Rule.** Larger radii communicate a larger bounded
surface; do not apply pill or prominent-surface rounding to ordinary buttons
for friendliness alone.

## Components

Components are quiet at rest and decisive in action. They favor direct
manipulation, visible mouse affordances, immediate optimistic feedback, and
clear rollback or conflict states.

### Buttons

- **Shape:** Gently rounded control corners with compact vertical rhythm.
- **Primary:** Glacier Signal Blue with white text, medium weight, and a
  restrained low shadow. Use one dominant primary action per local decision
  region.
- **Hover / Focus:** Hover may adjust tone or opacity; keyboard focus uses a
  visible Glacier Signal Blue ring. Disabled controls reduce emphasis and
  retain an understandable label.
- **Secondary / Quiet:** Secondary actions use a border or quiet surface.
  Inline property actions remain transparent until hover, focus, or open.

### Chips

- **Style:** Metadata chips use Quiet Surface, Slate Muted text, and a pill
  silhouette. They are compact annotations rather than miniature buttons.
- **State:** Filter triggers gain Glacier Signal Wash and blue text only while
  they actively narrow the collection.

### Cards / Containers

- **Corner Style:** Normal cards use medium corners; prominent header and login
  surfaces may use larger corners.
- **Background:** Cards sit on White Surface against Glacier Canvas.
- **Shadow Strategy:** Flat at rest. Selectable project cards may lift slightly
  on hover; ordinary information regions do not.
- **Border:** Soft Divider defines the boundary.
- **Internal Padding:** Normal cards use the Surface spacing step; prominent
  surfaces may use Section or Page spacing.

### Inputs / Fields

- **Style:** Persistent inputs use White Surface, Strong Control Stroke, and
  compact control corners. Inline task properties use transparent borders and
  backgrounds until interaction.
- **Focus:** Focus strengthens the blue border and adds a visible translucent
  ring without changing layout.
- **Error / Disabled:** Errors use Danger Red and Danger Wash. Read-only
  impersonation removes write affordances visually while the server remains
  authoritative.

### Navigation

Permanent navigation sits on Sidebar Mist. The active destination uses a solid
blue selection with white text; inactive items use muted text and a quiet white
hover. Medium layouts use the same navigation inside a drawer, while phones
switch to a compact bottom bar for primary destinations.

### Task Collection Row

Desktop rows are compact, divider-led scan lines with stable property columns.
Selection uses Glacier Signal Wash and a narrow blue leading indicator instead
of a surrounding card. Phone rows become two-line task cards while preserving
the same task properties and selection meaning.

### Named Rules

**The Quiet at Rest Rule.** A control may recede when its value reads clearly
as content, but hover, focus, open, selected, blocked, and error states must be
immediately recognizable.

## Do's and Don'ts

### Do:

- **Do** order information around the user's next decision and common work
  sequence.
- **Do** preserve fast inline editing and provide visible success, conflict,
  rollback, and undo feedback.
- **Do** use the shared semantic palette so List and Gantt communicate the same
  status, priority, selection, and danger meanings.
- **Do** use whitespace and dividers before introducing another container.
- **Do** improve existing surfaces incrementally while converging on these
  shared rules.

### Don't:

- **Don't** make the product resemble an administrative ledger merely because
  its underlying records are auditable.
- **Don't** hide the highest-priority risk or action beneath summary metadata,
  activity history, or entity chrome.
- **Don't** imitate chat-shaped task capture or encourage one-line requests.
- **Don't** turn every field, property, or content group into a bordered box or
  floating card.
- **Don't** use exaggerated motion, novelty interactions, decorative gradients,
  or gamification to manufacture modernity.
- **Don't** reduce desktop information density solely to satisfy mobile touch
  sizing; apply touch sizing according to input capability.
