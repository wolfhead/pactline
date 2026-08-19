import { cleanup, render, screen, within } from '@testing-library/react'
import { afterEach, describe, expect, it } from 'vitest'
import MarkdownContent from './MarkdownContent'

afterEach(cleanup)

describe('MarkdownContent', () => {
  it('renders GitHub-Flavored Markdown through its public reading interface', () => {
    const { container } = render(
      <MarkdownContent
        source={`# Release notes

- [x] Thread rendering
- [ ] Mobile verification

| Surface | State |
| --- | --- |
| Main Thread | Ready |

> Keep the source as Markdown.

Use ~~HTML~~ **Markdown** with \`inline code\`.

\`\`\`go
func main() {}
\`\`\`
`}
      />,
    )

    expect(screen.getByRole('heading', { name: 'Release notes', level: 1 })).toBeVisible()

    const checkboxes = screen.getAllByRole('checkbox')
    expect(checkboxes).toHaveLength(2)
    expect(checkboxes[0]).toBeChecked()
    expect(checkboxes[0]).toBeDisabled()
    expect(checkboxes[1]).not.toBeChecked()
    expect(checkboxes[1]).toBeDisabled()

    const table = screen.getByRole('table')
    expect(within(table).getByRole('columnheader', { name: 'Surface' })).toBeVisible()
    expect(within(table).getByRole('cell', { name: 'Main Thread' })).toBeVisible()
    expect(container.querySelector('blockquote')).toHaveTextContent('Keep the source as Markdown.')
    expect(container.querySelector('del')).toHaveTextContent('HTML')
    expect(container.querySelector('code:not(pre code)')).toHaveTextContent('inline code')
    expect(container.querySelector('pre code.language-go')).toHaveTextContent('func main() {}')
  })

  it('keeps authored HTML, dangerous links, and remote images inert', () => {
    const { container } = render(
      <MarkdownContent
        source={`<button onclick="alert('unsafe')">Unsafe HTML</button>

[Dangerous link](javascript:alert('unsafe'))

[Safe documentation](https://example.com/docs)

![Tracking pixel](https://tracker.example/pixel.gif)`}
      />,
    )

    expect(screen.queryByRole('button', { name: 'Unsafe HTML' })).not.toBeInTheDocument()
    expect(container.querySelector('script')).not.toBeInTheDocument()

    const dangerous = screen.getByText('Dangerous link')
    expect(dangerous).not.toHaveAttribute('href')

    expect(screen.getByRole('link', { name: 'Safe documentation' })).toHaveAttribute(
      'href',
      'https://example.com/docs',
    )
    expect(screen.getByRole('link', { name: 'Safe documentation' })).toHaveAttribute('target', '_blank')
    expect(screen.getByRole('link', { name: 'Safe documentation' })).toHaveAttribute(
      'rel',
      'noopener noreferrer',
    )

    expect(container.querySelector('img')).not.toBeInTheDocument()
    expect(screen.getByText('[外部图片已隐藏：Tracking pixel]')).toBeVisible()
  })

  it('contains wide Markdown structures inside the selected reading density', () => {
    const { container } = render(
      <MarkdownContent
        variant="document"
        source={`| A | B |
| --- | --- |
| one | two |

\`\`\`
a-very-long-code-line
\`\`\``}
      />,
    )

    expect(container.firstElementChild).toHaveClass('markdown-body--document')
    expect(within(container).getByRole('table').parentElement).toHaveClass('markdown-body__table-scroll')
    expect(container.querySelector('pre')?.parentElement).toHaveClass('markdown-body__code-scroll')
  })
})
