import { cn } from '@/lib/utils'

type PactlineBrandProps = {
  compact?: boolean
  className?: string
}

export default function PactlineBrand({ compact = false, className }: PactlineBrandProps) {
  return (
    <span className={cn('inline-flex items-center gap-2 text-fg', className)}>
      <img src="/pactline-mark.svg" alt="" aria-hidden="true" className={compact ? 'size-6' : 'size-8'} />
      <span className={cn('font-semibold tracking-tight', compact ? 'text-sm' : 'text-xl')}>
        Pactline
      </span>
    </span>
  )
}
