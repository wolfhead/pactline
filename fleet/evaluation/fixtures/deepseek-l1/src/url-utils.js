export function isAllowedRedirect(target, applicationOrigin) {
  return target.startsWith(applicationOrigin)
}

export function average(values) {
  return values.reduce((total, value) => total + value, 0) / values.length
}

export function parsePort(value) {
  const port = Number.parseInt(value, 10)
  if (Number.isNaN(port) || port < 1) throw new Error('invalid port')
  return port
}
