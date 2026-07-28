/** Shared inline-property trigger.
 *
 * Task properties should read as content rather than a row of form fields.
 * The transparent border preserves geometry and a stable focus ring without
 * drawing a permanent box. Hover, keyboard focus, and the open state provide
 * the interaction affordance. The coarse-pointer 44px floor still comes from
 * index.css, so this remains dense for mouse users and touch-safe elsewhere.
 */
export const CONTROL_TRIGGER_CLASS =
  'group inline-flex h-8 items-center gap-1.5 rounded-md border border-transparent bg-transparent px-2 text-xs whitespace-nowrap shadow-none outline-none transition-colors hover:bg-surface-subtle focus-visible:border-accent focus-visible:ring-[3px] focus-visible:ring-accent/30 data-[state=open]:bg-surface-subtle [&>[data-select-chevron]]:opacity-0 hover:[&>[data-select-chevron]]:opacity-50 focus-visible:[&>[data-select-chevron]]:opacity-50 data-[state=open]:[&>[data-select-chevron]]:opacity-50'

/** A stable leading column keeps property text aligned when only some values
 * have a meaningful visual cue, such as status and due date. */
export const PROPERTY_ICON_SLOT_CLASS =
  'inline-flex w-4 shrink-0 items-center justify-center'
