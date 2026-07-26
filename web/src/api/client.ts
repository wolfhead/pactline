/**
 * Phase 1 has no authentication. The current identity is carried in an
 * X-User-Id header set by the user switcher. Both disappear when Feishu OAuth
 * lands in Phase 6.
 */
let currentUserId = ''

export function setCurrentUserId(id: string): void {
  currentUserId = id
}

export function getCurrentUserId(): string {
  return currentUserId
}

export class ApiError extends Error {
  constructor(
    message: string,
    readonly status: number,
  ) {
    super(message)
    this.name = 'ApiError'
  }
}

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  const res = await fetch(path, {
    method,
    headers: {
      'Content-Type': 'application/json',
      'X-User-Id': currentUserId,
    },
    body: body === undefined ? undefined : JSON.stringify(body),
  })

  const text = await res.text()

  // The body is not guaranteed to be JSON: the Vite dev proxy returns an
  // HTML error page when the Go backend is unreachable, and some upstream
  // failures (502s from a reverse proxy, etc.) do the same. A raw
  // JSON.parse() would throw a SyntaxError that escapes as-is, bypassing
  // ApiError entirely and confusing callers that check `instanceof ApiError`
  // or read `.status`. Guard the parse so both a failed response and a
  // successful-but-unparseable one always surface as an ApiError.
  let parsed: unknown = null
  if (text) {
    try {
      parsed = JSON.parse(text)
    } catch (err) {
      console.error('response body is not valid JSON', err)
      const snippet = text.length > 200 ? `${text.slice(0, 200)}…` : text
      throw new ApiError(`${res.status} ${res.statusText}: ${snippet}`, res.status)
    }
  }

  if (!res.ok) {
    const errorField = parsed && typeof parsed === 'object' ? (parsed as { error?: unknown }).error : undefined
    const message = typeof errorField === 'string' ? errorField : res.statusText
    throw new ApiError(message, res.status)
  }
  return parsed as T
}

export function apiGet<T>(path: string): Promise<T> {
  return request<T>('GET', path)
}

export function apiPost<T>(path: string, body?: unknown): Promise<T> {
  return request<T>('POST', path, body)
}
