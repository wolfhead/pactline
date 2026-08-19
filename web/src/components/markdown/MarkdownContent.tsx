import ReactMarkdown, { type Components } from 'react-markdown'
import remarkGfm from 'remark-gfm'
import { cn } from '@/lib/utils'
import './markdown.css'

interface MarkdownContentProps {
  source: string
  variant?: 'compact' | 'document'
  className?: string
}

const COMPONENTS: Components = {
  a: ({ href, children, title }) => (
    href
      ? <a href={href} title={title} target="_blank" rel="noopener noreferrer">{children}</a>
      : <span>{children}</span>
  ),
  img: ({ alt }) => (
    <span className="markdown-body__hidden-image">
      [外部图片已隐藏{alt ? `：${alt}` : ''}]
    </span>
  ),
  table: ({ children }) => (
    <div className="markdown-body__table-scroll" role="region" aria-label="Markdown 表格" tabIndex={0}>
      <table>{children}</table>
    </div>
  ),
  pre: ({ children }) => (
    <div className="markdown-body__code-scroll" role="region" aria-label="代码块" tabIndex={0}>
      <pre>{children}</pre>
    </div>
  ),
}

export default function MarkdownContent({
  source,
  variant = 'compact',
  className,
}: MarkdownContentProps) {
  return (
    <div className={cn('markdown-body', `markdown-body--${variant}`, className)}>
      <ReactMarkdown remarkPlugins={[remarkGfm]} components={COMPONENTS}>{source}</ReactMarkdown>
    </div>
  )
}
