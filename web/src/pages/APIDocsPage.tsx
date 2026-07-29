import { useState } from 'react'
import SwaggerUI from 'swagger-ui-react'
import 'swagger-ui-react/swagger-ui.css'

const ALL_METHODS = ['get', 'put', 'post', 'delete', 'options', 'head', 'patch', 'trace'] as const

export default function APIDocsPage() {
  const [acknowledged, setAcknowledged] = useState(false)

  return (
    <div className="mx-auto flex w-full max-w-7xl flex-col gap-4 p-4 sm:p-6">
      <header>
        <h1 className="text-xl font-semibold">API 文档</h1>
        <p className="mt-1 text-sm text-fg-muted">
          契约来自当前服务的 OpenAPI 3.1 文档。Agent 应使用个人 Bearer Token，并为写请求提供幂等键和并发版本。
        </p>
      </header>
      <section className="rounded-lg border border-border bg-surface-raised p-4">
        <label className="flex items-start gap-3 text-sm">
          <input
            type="checkbox"
            checked={acknowledged}
            onChange={(event) => setAcknowledged(event.target.checked)}
            className="mt-0.5 size-4"
          />
          <span>
            我了解“Try it out”会直接修改当前开发环境中的真实数据。
            {!acknowledged && <span className="mt-1 block text-fg-muted">确认前，文档仅可阅读，所有在线请求按钮均已禁用。</span>}
          </span>
        </label>
      </section>
      <div className="overflow-hidden rounded-lg border border-border bg-surface">
        <SwaggerUI
          url="/api/openapi.yaml"
          withCredentials
          requestInterceptor={(request) => {
            request.credentials = 'same-origin'
            return request
          }}
          supportedSubmitMethods={acknowledged ? [...ALL_METHODS] : []}
          displayOperationId
          displayRequestDuration
          docExpansion="list"
          defaultModelsExpandDepth={1}
          persistAuthorization={false}
        />
      </div>
    </div>
  )
}
