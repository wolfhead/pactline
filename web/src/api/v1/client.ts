export class ProblemError extends Error {
  constructor(
    readonly status: number,
    readonly code: string,
    readonly requestId: string,
    readonly currentVersion?: number,
  ) {
    super(code)
    this.name = 'ProblemError'
  }
}

export interface Versioned<T> {
  value: T
  etag: string
}

export interface V1Response<T> {
  value: T
  etag?: string
  requestId: string
}

export interface V1RequestOptions {
  body?: unknown
  ifMatch?: string
  projectIfMatch?: string
}

function cookieValue(name: string): string | null {
  const prefix = `${encodeURIComponent(name)}=`
  for (const part of document.cookie.split(';')) {
    const value = part.trim()
    if (value.startsWith(prefix)) return decodeURIComponent(value.slice(prefix.length))
  }
  return null
}

function problemMessage(value: unknown, fallback: string): string {
  if (!value || typeof value !== 'object') return fallback
  const problem = value as { title?: unknown; detail?: unknown }
  if (typeof problem.detail === 'string' && problem.detail) return problem.detail
  if (typeof problem.title === 'string' && problem.title) return problem.title
  return fallback
}

async function request<T>(
  method: string,
  path: string,
  options: V1RequestOptions = {},
): Promise<V1Response<T>> {
  const headers: Record<string, string> = {}
  if (options.body !== undefined) headers['Content-Type'] = 'application/json'
  if (options.ifMatch) headers['If-Match'] = options.ifMatch
  if (options.projectIfMatch) headers['X-Project-If-Match'] = options.projectIfMatch
  if (method !== 'GET' && method !== 'HEAD') {
    const csrfToken = cookieValue('bb_csrf')
    if (csrfToken) headers['X-CSRF-Token'] = csrfToken
  }

  const response = await fetch(path, {
    method,
    credentials: 'same-origin',
    headers,
    body: options.body === undefined ? undefined : JSON.stringify(options.body),
  })
  const text = await response.text()
  let parsed: unknown = null
  if (text) {
    try {
      parsed = JSON.parse(text)
    } catch (error) {
      console.error('v1 response body is not valid JSON', {
        path, status: response.status, error,
      })
      throw new ProblemError(
        response.status,
        'INVALID_RESPONSE',
        response.headers.get('X-Request-ID') ?? '',
      )
    }
  }

  if (!response.ok) {
    const problem = parsed && typeof parsed === 'object'
      ? parsed as {
          code?: unknown
          request_id?: unknown
          current_version?: unknown
        }
      : {}
    const code = typeof problem.code === 'string' ? problem.code : 'UNKNOWN_ERROR'
    const requestId = typeof problem.request_id === 'string'
      ? problem.request_id
      : (response.headers.get('X-Request-ID') ?? '')
    const currentVersion = typeof problem.current_version === 'number'
      ? problem.current_version
      : undefined
    const error = new ProblemError(response.status, code, requestId, currentVersion)
    error.message = problemMessage(parsed, response.statusText || code)
    throw error
  }

  return {
    value: parsed as T,
    etag: response.headers.get('ETag') ?? undefined,
    requestId: response.headers.get('X-Request-ID') ?? '',
  }
}

export function v1Get<T>(path: string): Promise<V1Response<T>> {
  return request<T>('GET', path)
}

export function v1Post<T>(
  path: string,
  options: V1RequestOptions = {},
): Promise<V1Response<T>> {
  return request<T>('POST', path, options)
}

export function v1Patch<T>(
  path: string,
  options: V1RequestOptions,
): Promise<V1Response<T>> {
  return request<T>('PATCH', path, options)
}

export function v1Delete(
  path: string,
  options: V1RequestOptions,
): Promise<V1Response<void>> {
  return request<void>('DELETE', path, options)
}

export function requireVersioned<T>(response: V1Response<T>): Versioned<T> {
  if (!response.etag) {
    throw new ProblemError(500, 'MISSING_ETAG', response.requestId)
  }
  return { value: response.value, etag: response.etag }
}

export function etagForVersion(version: number): string {
  return `"${version}"`
}
