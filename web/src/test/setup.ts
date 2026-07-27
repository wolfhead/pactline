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
