/** Shared trigger sizing/shape for every permanently-visible property
 * control — Status/Priority/Assignee's `Select` trigger and DueDate/Label's
 * `Popover` trigger alike — so all five controls sit uniformly in a row.
 * `h-8` is the mouse size; the 44px coarse-pointer floor is NOT stated here.
 * It comes from index.css's single `@media (pointer: coarse)` base rule, and
 * a `min-h-*` utility here would outrank that rule rather than agree with it.
 *
 * `border-border-strong`, not `border-border`: this is a real UI-component
 * boundary, which WCAG 1.4.11 holds to 3:1. `--color-border` is the hairline
 * divider token and measures 1.25:1 light / 1.26:1 dark against the trigger's
 * own surface — index.css's own token comment says as much, and that comment
 * is the whole justification for exempting `--color-border` from the contrast
 * sweep. Stating it here also settles the tailwind-merge race with
 * `SelectTrigger`, whose own `border-border-strong` this class arrives as
 * `className` on top of and used to overwrite.
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
  'inline-flex h-8 items-center gap-1.5 rounded-md border border-border-strong bg-surface px-2 text-xs whitespace-nowrap'
