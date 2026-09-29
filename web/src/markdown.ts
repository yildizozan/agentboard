import DOMPurify from 'dompurify'
import { marked } from 'marked'

// Agents write Markdown, so it is sanitized before it becomes HTML; links open outside the board.
DOMPurify.addHook('afterSanitizeAttributes', (node) => {
  if (node instanceof HTMLAnchorElement) {
    node.target = '_blank'
    node.rel = 'noopener noreferrer'
  }
})

// renderMarkdown returns sanitized HTML for a card body.
export function renderMarkdown(body: string): string {
  return DOMPurify.sanitize(marked.parse(body, { async: false }))
}
