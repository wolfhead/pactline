import * as React from 'react'
import { HoverCard as HoverCardPrimitive } from 'radix-ui'

import { useOverlayPortalContainer } from '@/components/ui/overlay-portal'
import { cn } from '@/lib/utils'

function HoverCard(props: React.ComponentProps<typeof HoverCardPrimitive.Root>) {
  return <HoverCardPrimitive.Root data-slot="hover-card" {...props} />
}

function HoverCardTrigger(props: React.ComponentProps<typeof HoverCardPrimitive.Trigger>) {
  return <HoverCardPrimitive.Trigger data-slot="hover-card-trigger" {...props} />
}

function HoverCardContent({
  className,
  align = 'start',
  sideOffset = 8,
  ...props
}: React.ComponentProps<typeof HoverCardPrimitive.Content>) {
  const portalContainer = useOverlayPortalContainer()

  return (
    <HoverCardPrimitive.Portal container={portalContainer ?? undefined}>
      <HoverCardPrimitive.Content
        data-slot="hover-card-content"
        align={align}
        sideOffset={sideOffset}
        collisionPadding={12}
        className={cn(
          'z-50 w-80 origin-(--radix-hover-card-content-transform-origin) rounded-lg border border-border-strong bg-surface-raised p-3 text-fg shadow-[0_12px_32px_rgb(23_43_61/0.16)] outline-none',
          'data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=closed]:zoom-out-95',
          'data-[state=open]:animate-in data-[state=open]:fade-in-0 data-[state=open]:zoom-in-95',
          className,
        )}
        {...props}
      />
    </HoverCardPrimitive.Portal>
  )
}

export { HoverCard, HoverCardContent, HoverCardTrigger }
