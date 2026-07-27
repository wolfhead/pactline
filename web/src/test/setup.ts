import '@testing-library/jest-dom/vitest'

// jsdom implements neither Element.scrollIntoView nor the pointer-capture
// trio; Radix's Select calls all of them when it opens (to scroll the
// selected item into view) and when it manages pointer interactions on the
// trigger/items. Without stubs, every test that opens a Radix Select throws
// "not a function" from inside Radix's own effects, unrelated to anything
// under test here.
if (!Element.prototype.scrollIntoView) {
  Element.prototype.scrollIntoView = () => {}
}
if (!Element.prototype.hasPointerCapture) {
  Element.prototype.hasPointerCapture = () => false
}
if (!Element.prototype.setPointerCapture) {
  Element.prototype.setPointerCapture = () => {}
}
if (!Element.prototype.releasePointerCapture) {
  Element.prototype.releasePointerCapture = () => {}
}

// jsdom 25 does not implement the PointerEvent constructor at all (unlike
// scrollIntoView/pointer-capture above, which exist as no-op stubs to add,
// this one is missing outright). Radix's DropdownMenu/Popover-family
// triggers open on `pointerdown` with no `click` fallback (unlike Select's
// trigger, which composes both), so a real interaction test for one of them
// needs `fireEvent.pointerDown` to carry a genuine PointerEvent — without
// this, @testing-library/dom's event-map falls back to a bare `Event`,
// whose `.button`/`.ctrlKey` read as `undefined` and never satisfy Radix's
// `event.button === 0 && event.ctrlKey === false` guard. Extending
// MouseEvent (which jsdom does implement) covers exactly the properties
// Radix's own trigger/item handlers read.
if (typeof window !== 'undefined' && !window.PointerEvent) {
  class PointerEventPolyfill extends MouseEvent {
    pointerId: number
    pointerType: string
    isPrimary: boolean

    constructor(type: string, params: PointerEventInit = {}) {
      super(type, params)
      this.pointerId = params.pointerId ?? 0
      this.pointerType = params.pointerType ?? 'mouse'
      this.isPrimary = params.isPrimary ?? true
    }
  }
  // @ts-expect-error — assigning a partial polyfill to the DOM lib's full
  // PointerEvent type; sufficient for the properties Radix's handlers read.
  window.PointerEvent = PointerEventPolyfill
}
