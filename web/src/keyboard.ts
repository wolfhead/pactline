/**
 * True when a keyboard event originates from a text-editing control, so
 * single-key global shortcuts (c, /, j, k, ?) don't fire while the user is
 * typing into an input, textarea, select or contenteditable element.
 */
export function isTypingTarget(target: EventTarget | null): boolean {
  if (!(target instanceof HTMLElement)) return false
  const tag = target.tagName
  return tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT' || target.isContentEditable
}
