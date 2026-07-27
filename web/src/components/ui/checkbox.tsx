import * as React from "react"
import { CheckIcon } from "lucide-react"
import { Checkbox as CheckboxPrimitive } from "radix-ui"

import { cn } from "@/lib/utils"

function Checkbox({
  className,
  ...props
}: React.ComponentProps<typeof CheckboxPrimitive.Root>) {
  return (
    <CheckboxPrimitive.Root
      data-slot="checkbox"
      className={cn(
        // p-0: Radix renders this as a <button>, and styles.css's legacy
        // `button { padding: var(--sp-2) var(--sp-4) }` rule (unopposed,
        // since this component never otherwise sets padding) inflates the
        // UA-computed auto min-size a `<button>` clamps its own width/height
        // to — `size-4` alone then loses that clamp and renders ~34x18
        // instead of a 16x16 square. Same root cause, same fix, as
        // TaskDetail's close button (see index.css's `layer(legacy)`
        // comment); caught the same way, by screenshot, in Task 10.
        "peer size-4 shrink-0 rounded-[4px] border border-border-strong p-0 shadow-xs transition-shadow outline-none focus-visible:border-accent focus-visible:ring-[3px] focus-visible:ring-accent/50 disabled:cursor-not-allowed disabled:opacity-50 aria-invalid:border-danger aria-invalid:ring-danger/20 data-[state=checked]:border-accent data-[state=checked]:bg-accent data-[state=checked]:text-accent-fg",
        className
      )}
      {...props}
    >
      <CheckboxPrimitive.Indicator
        data-slot="checkbox-indicator"
        className="grid place-content-center text-current transition-none"
      >
        <CheckIcon className="size-3.5" />
      </CheckboxPrimitive.Indicator>
    </CheckboxPrimitive.Root>
  )
}

export { Checkbox }
