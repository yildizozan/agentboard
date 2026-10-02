// Modal Markdown editor shared by "add task" and "edit task".

import type { Kind, Priority } from './api'

// EditorFields adds selects above the body. kind is shown only when set, i.e. for new cards.
export interface EditorFields {
  kind?: Kind
  cardKind?: Kind // existing card kind; does not show a kind selector
  priority: Priority
  priorities: Priority[]
  epic: number | null
  epics: { id: number; title: string }[]
}

// EditorValues is what the editor saves; the field values are present only with fields.
export interface EditorValues {
  body: string
  kind?: Kind
  priority?: Priority
  epic?: number | null
}

export interface EditorOptions {
  heading: string
  body: string
  submitLabel: string
  fields?: EditorFields
  // true closes the editor; false or an error message preserves the draft.
  save: (values: EditorValues) => Promise<boolean | string>
  onClose: () => void
}

function selectOf(label: string, options: [value: string, text: string][], value: string): HTMLSelectElement {
  const select = document.createElement('select')
  select.setAttribute('aria-label', label)
  for (const [v, text] of options) {
    const option = document.createElement('option')
    option.value = v
    option.textContent = text
    select.append(option)
  }
  select.value = value
  return select
}

// fieldsRow builds the selects and returns them with a reader for their values.
function fieldsRow(f: EditorFields): { row: HTMLElement; values: () => Omit<EditorValues, 'body'> } {
  const row = document.createElement('div')
  row.className = 'editor-fields'
  const priority = selectOf('Priority', f.priorities.map((p) => [p, p]), f.priority)
  const epic = selectOf('Epic', [['', 'Select an epic'], ...f.epics.map((e): [string, string] => [String(e.id), `#${e.id} ${e.title}`])],
    f.epic === null ? '' : String(f.epic))
  const kind = f.kind === undefined ? null : selectOf('Kind', [['task', 'Task'], ['epic', 'Epic']], f.kind)
  // An epic cannot belong to another epic.
  const syncEpic = () => {
    epic.disabled = (kind?.value ?? f.cardKind ?? 'task') === 'epic'
    epic.required = !epic.disabled
    if (epic.disabled) epic.value = ''
    epic.setCustomValidity(!epic.disabled && f.epics.length === 0 ? 'Create an epic before adding a task.' : '')
  }
  kind?.addEventListener('change', syncEpic)
  syncEpic()
  const labeled = (text: string, select: HTMLSelectElement) => {
    const label = document.createElement('label')
    label.className = 'editor-field'
    label.append(text, select)
    return label
  }
  if (kind) row.append(labeled('Kind', kind))
  row.append(labeled('Priority', priority), labeled('Epic', epic))
  return {
    row,
    values: () => ({
      ...(kind ? { kind: kind.value as Kind } : {}),
      priority: priority.value as Priority,
      epic: epic.value === '' ? null : Number(epic.value),
    }),
  }
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

  const fields = opts.fields ? fieldsRow(opts.fields) : null
  const error = document.createElement('p')
  error.className = 'editor-error'
  error.setAttribute('role', 'alert')
  error.hidden = true
  const form = document.createElement('form')
  form.className = 'editor-form'
  form.append(head, ...(fields ? [fields.row] : []), error, body, hint)
  let saving = false
  form.addEventListener('submit', async (e) => {
    e.preventDefault()
    if (saving || signal.aborted) return
    saving = true
    error.hidden = true
    const values = { body: body.value, ...fields?.values() }
    const selects = [...form.querySelectorAll('select')].map((select) => ({ select, disabled: select.disabled }))
    body.readOnly = true
    selects.forEach(({ select }) => { select.disabled = true })
    save.disabled = true
    try {
      const result = await opts.save(values)
      if (signal.aborted) return
      if (result === true) dialog.close()
      else if (typeof result === 'string') {
        error.textContent = result
        error.hidden = false
      }
    } catch {
      if (!signal.aborted) {
        error.textContent = 'The task could not be saved. Your draft is still here; try again.'
        error.hidden = false
      }
    } finally {
      saving = false
      save.disabled = false
      body.readOnly = false
      selects.forEach(({ select, disabled }) => { select.disabled = disabled })
    }
  })
  body.addEventListener('keydown', (e) => {
    if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) {
      e.preventDefault()
      if (!saving) form.requestSubmit()
    }
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
