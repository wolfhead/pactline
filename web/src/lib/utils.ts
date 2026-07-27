import { clsx, type ClassValue } from 'clsx'
import { twMerge } from 'tailwind-merge'

/** Merges class lists so a later Tailwind utility wins over an earlier one of
 * the same kind — `cn('p-2', 'p-4')` yields `p-4`, not both. */
export function cn(...inputs: ClassValue[]): string {
  return twMerge(clsx(inputs))
}
