import { afterEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import APIDocsPage from './APIDocsPage'

const swaggerProps = vi.hoisted(() => ({ current: {} as Record<string, unknown> }))

vi.mock('swagger-ui-react', () => ({
  default: (props: Record<string, unknown>) => {
    swaggerProps.current = props
    return <div data-testid="swagger-ui" />
  },
}))

afterEach(() => {
  cleanup()
  vi.clearAllMocks()
})

describe('APIDocsPage', () => {
  it('keeps live requests disabled until the destructive-action warning is acknowledged', () => {
    render(<APIDocsPage />)

    expect(swaggerProps.current.url).toBe('/api/openapi.yaml')
    expect(swaggerProps.current.withCredentials).toBe(true)
    expect(swaggerProps.current.supportedSubmitMethods).toEqual([])

    const interceptor = swaggerProps.current.requestInterceptor as (request: Record<string, unknown>) => Record<string, unknown>
    expect(interceptor({}).credentials).toBe('same-origin')

    fireEvent.click(screen.getByRole('checkbox'))
    expect(swaggerProps.current.supportedSubmitMethods).toContain('delete')
    expect(swaggerProps.current.supportedSubmitMethods).toContain('patch')
  })
})
