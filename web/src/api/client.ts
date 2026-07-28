export class ApiError extends Error {
  constructor(
    message: string,
    readonly status: number,
  ) {
    super(message)
    this.name = 'ApiError'
  }
}

function cookieValue(name: string): string | null {
  const prefix = `${encodeURIComponent(name)}=`
  for (const part of document.cookie.split(';')) {
    const value = part.trim()
    if (value.startsWith(prefix)) return decodeURIComponent(value.slice(prefix.length))
  }
  return null
}

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  const headers: Record<string, string> = {}
  if (body !== undefined) headers['Content-Type'] = 'application/json'
  if (method !== 'GET' && method !== 'HEAD') {
    const csrfToken = cookieValue('bb_csrf')
    if (csrfToken) headers['X-CSRF-Token'] = csrfToken
  }

  const res = await fetch(path, {
    method,
    credentials: 'same-origin',
    headers,
    body: body === undefined ? undefined : JSON.stringify(body),
  })
  const text = await res.text()

  let parsed: unknown = null
  if (text) {
    try {
      parsed = JSON.parse(text)
    } catch (error) {
      console.error('response body is not valid JSON', { path, status: res.status, error })
      const snippet = text.length > 200 ? `${text.slice(0, 200)}…` : text
      throw new ApiError(`${res.status} ${res.statusText}: ${snippet}`, res.status)
    }
  }

  if (!res.ok) {
    const errorField = parsed && typeof parsed === 'object' ? (parsed as { error?: unknown }).error : undefined
    throw new ApiError(typeof errorField === 'string' ? errorField : res.statusText, res.status)
  }
  return parsed as T
}

export function apiGet<T>(path: string): Promise<T> {
  return request<T>('GET', path)
}

export function apiPost<T>(path: string, body?: unknown): Promise<T> {
  return request<T>('POST', path, body)
}

export function apiPatch<T>(path: string, body?: unknown): Promise<T> {
  return request<T>('PATCH', path, body)
}

export function apiDelete<T>(path: string, body?: unknown): Promise<T> {
  return request<T>('DELETE', path, body)
}
