// Modal Markdown editor shared by "add task" and "edit task".

export interface EditorOptions {
  heading: string
  body: string
  submitLabel: string
  // save resolves true when the body was stored; on false the editor stays open with its text.
  save: (body: string) => Promise<boolean>
  onClose: () => void
}

export function openEditor(dialog: HTMLDialogElement, opts: EditorOptions) {
  // Scopes this opening's close listener, so reopening the same dialog starts clean.
  const current = new AbortController()
  const { signal } = current

  const body = document.createElement('textarea')
  body.className = 'editor-body'
  body.value = opts.body
  body.spellcheck = false
  body.placeholder = '# Task title\n\n## Context\nDetails in Markdown'
  body.setAttribute('aria-label', 'Body (Markdown, first line is the # title)')

  const heading = document.createElement('h2')
  heading.id = 'editor-heading'
  heading.textContent = opts.heading

  const save = document.createElement('button')
  save.type = 'submit'
  save.textContent = opts.submitLabel
  const cancel = document.createElement('button')
  cancel.type = 'button'
  cancel.textContent = 'Cancel'
  cancel.addEventListener('click', () => dialog.close())

  const actions = document.createElement('div')
  actions.className = 'edit-actions'
  actions.append(cancel, save)
  const head = document.createElement('div')
  head.className = 'editor-head'
  head.append(heading, actions)
  const hint = document.createElement('p')
  hint.className = 'editor-hint'
  hint.textContent = 'Markdown. The first line is the "# title". Cmd/Ctrl+Enter saves, Esc cancels.'

  const form = document.createElement('form')
  form.className = 'editor-form'
  form.append(head, body, hint)
  form.addEventListener('submit', async (e) => {
    e.preventDefault()
    save.disabled = true
    const ok = await opts.save(body.value)
    save.disabled = false
    if (ok && !signal.aborted) dialog.close()
  })
  body.addEventListener('keydown', (e) => {
    if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) form.requestSubmit()
  })
  dialog.addEventListener(
    'close',
    () => {
      current.abort()
      opts.onClose()
    },
    { signal },
  )

  dialog.setAttribute('aria-labelledby', heading.id)
  dialog.replaceChildren(form)
  if (!dialog.open) dialog.showModal()
  body.focus()
  body.setSelectionRange(0, 0)
}
