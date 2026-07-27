/** Shared trigger sizing/shape for every permanently-visible property
 * control — Status/Priority/Assignee's `Select` trigger and DueDate/Label's
 * `Popover` trigger alike — so all five controls sit uniformly in a row.
 * `min-h-11`/`pointer-coarse:min-h-11` keep the touch target at 44px on
 * coarse pointers while `sm:min-h-8` lets it shrink back down for mouse
 * users; Task 14's Playwright suite measures this for real. */
export const CONTROL_TRIGGER_CLASS =
  'h-8 min-h-11 gap-1.5 border-border bg-surface px-2 text-xs pointer-coarse:min-h-11 sm:min-h-8'
