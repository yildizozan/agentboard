// @vitest-environment jsdom
import { describe, expect, it } from 'vitest'
import { renderMarkdown } from './markdown'

// render parses the HTML the board would insert, so assertions see the resulting DOM.
function render(body: string): HTMLElement {
  const root = document.createElement('div')
  root.innerHTML = renderMarkdown(body)
  return root
}

describe('renderMarkdown', () => {
  it('renders headings, code and task lists', () => {
    const root = render('# Fix login\n\n## Steps\n- [x] test `auth.go`\n- [ ] fix')
    expect(root.querySelector('h1')?.textContent).toBe('Fix login')
    expect(root.querySelector('h2')?.textContent).toBe('Steps')
    expect(root.querySelector('code')?.textContent).toBe('auth.go')
    const boxes = [...root.querySelectorAll<HTMLInputElement>('input[type=checkbox]')]
    expect(boxes.map((b) => b.checked)).toEqual([true, false])
  })

  it('removes scripts and event handlers written into a card', () => {
    const root = render('# x\n\n<script>alert(1)</script><img src="x" onerror="alert(2)"><div onclick="alert(3)">hi</div>')
    expect(root.querySelector('script')).toBeNull()
    expect(root.querySelector('img')?.hasAttribute('onerror')).toBe(false)
    expect(root.querySelector('div')?.hasAttribute('onclick')).toBe(false)
  })

  it('drops javascript: links', () => {
    const root = render('[click](javascript:alert(1))')
    expect(root.querySelector('a')?.getAttribute('href') ?? null).toBeNull()
  })

  it('opens links outside the board without giving the page an opener', () => {
    const link = render('[docs](https://example.com)').querySelector('a')
    expect(link?.getAttribute('href')).toBe('https://example.com')
    expect(link?.getAttribute('target')).toBe('_blank')
    expect(link?.getAttribute('rel')).toBe('noopener noreferrer')
  })
})
