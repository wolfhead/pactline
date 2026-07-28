/** Shared trigger sizing/shape for every permanently-visible property
 * control — Status/Priority/Assignee's `Select` trigger and DueDate/Label's
 * `Popover` trigger alike — so all five controls sit uniformly in a row.
 * `min-h-11`/`pointer-coarse:min-h-11` keep the touch target at 44px on
 * coarse pointers while `sm:min-h-8` lets it shrink back down for mouse
 * users; Task 14's Playwright suite measures this for real.
 *
 * The box itself — inline-flex, the 1px border, the radius — is stated here
 * rather than assumed. shadcn's `SelectTrigger` brings its own; Radix's
 * `PopoverTrigger` and `DropdownMenuTrigger` render a completely bare
 * <button>, which until Task 13 was dressed by styles.css's element-level
 * `button { padding; border; border-radius }` rule. With that stylesheet
 * gone and preflight on (border-width 0, padding 0, and svg children set to
 * `display: block`), those triggers rendered as unboxed text and the
 * "＋ 新建任务" button broke its icon onto a line of its own. Keep all four
 * of inline-flex / items-center / border / rounded-md here.
 */
export const CONTROL_TRIGGER_CLASS =
  'inline-flex h-8 min-h-11 items-center gap-1.5 rounded-md border border-border bg-surface px-2 text-xs whitespace-nowrap pointer-coarse:min-h-11 sm:min-h-8'
