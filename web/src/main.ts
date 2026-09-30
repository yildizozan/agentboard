import './style.css'
import { api, ApiError, type Board, type Repo, type Status, type Task } from './api'
import { dropAction } from './drop'
import { openEditor, type EditorFields, type EditorValues } from './editor'
import { cardHue } from './epic'
import { renderMarkdown } from './markdown'
import { boardPath, repoFromPath } from './route'

const POLL_MS = 2000
const TOAST_MS = 4000

const state = {
  repos: [] as Repo[],
  repo: repoFromPath(location.pathname),
  board: null as Board | null,
  dragging: null as { id: number; from: Status } | null,
  openId: null as number | null, // task shown in the detail dialog
  editing: false, // the editor dialog is open; polling pauses so it is not disturbed
}

function byId<T extends HTMLElement>(id: string): T {
  const node = document.getElementById(id)
  if (!node) throw new Error(`#${id} missing from index.html`)
  return node as T
}

const ui = {
  select: byId<HTMLSelectElement>('repo'),
  columns: byId<HTMLDivElement>('columns'),
  empty: byId<HTMLParagraphElement>('empty'),
  addButton: byId<HTMLButtonElement>('add-task'),
  detail: byId<HTMLDialogElement>('detail'),
  editor: byId<HTMLDialogElement>('editor'),
  toast: byId<HTMLDivElement>('toast'),
}

// el builds an element; text children go through textContent, so task data is never parsed as HTML.
function el<K extends keyof HTMLElementTagNameMap>(
  tag: K,
  className = '',
  ...children: (Node | string)[]
): HTMLElementTagNameMap[K] {
  const node = document.createElement(tag)
  if (className) node.className = className
  node.append(...children)
  return node
}

function button(label: string, className: string, onClick: () => void, ariaLabel = label): HTMLButtonElement {
  const b = el('button', className, label)
  b.type = 'button'
  b.setAttribute('aria-label', ariaLabel)
  b.addEventListener('click', onClick)
  return b
}

let toastTimer = 0
function toast(message: string) {
  ui.toast.textContent = message
  ui.toast.hidden = false
  clearTimeout(toastTimer)
  toastTimer = window.setTimeout(() => (ui.toast.hidden = true), TOAST_MS)
}

function errorMessage(err: unknown): string {
  if (err instanceof ApiError) return err.message
  return 'Cannot reach agentboard. Is `agentboard board` still running?'
}

function markdown(body: string): HTMLElement {
  const node = el('div', 'markdown')
  node.innerHTML = renderMarkdown(body)
  return node
}

let boardKey = ''
async function refresh() {
  try {
    state.repos = await api.repos()
    if (!state.repo && state.repos.length > 0) setRepo(state.repos[0].path)
    state.board = state.repo ? await api.board(state.repo) : null
    const key = JSON.stringify([state.repo, state.repos, state.board])
    if (key === boardKey) return // unchanged poll: keep the DOM, so selections and scroll survive
    boardKey = key
    render()
  } catch (err) {
    toast(errorMessage(err))
  }
}

function setRepo(path: string) {
  state.repo = path
  closeDetail()
  history.replaceState(null, '', boardPath(path))
}

function render() {
  renderSelect()
  ui.addButton.disabled = !state.repo
  if (!state.board) {
    ui.columns.replaceChildren()
    ui.empty.hidden = false
    ui.empty.textContent = 'No boards yet. A board appears when an agent or the CLI adds its first task.'
    renderDetail()
    return
  }
  ui.empty.hidden = true
  const { statuses, tasks } = state.board
  ui.columns.replaceChildren(...statuses.map((s) => column(s, tasks.filter((t) => t.status === s))))
  renderDetail()
}

let selectKey = ''
function renderSelect() {
  const repos = [...state.repos]
  // Keep a board selected from the URL even before it has tasks, so its first task can be added here.
  if (state.repo && !repos.some((r) => r.path === state.repo)) {
    repos.push({ path: state.repo, name: state.repo.split('/').pop() || state.repo, count: 0 })
  }
  const key = JSON.stringify([state.repo, repos])
  if (key === selectKey) return // rebuilding would close an open dropdown on every poll
  selectKey = key
  ui.select.replaceChildren(
    ...repos.map((r) => {
      const opt = el('option', '', `${r.name} (${r.count})`)
      opt.value = r.path
      opt.title = r.path
      opt.selected = r.path === state.repo
      return opt
    }),
  )
}

function column(status: Status, tasks: Task[]): HTMLElement {
  const list = el('div', 'cards', ...tasks.map(card))
  const col = el('section', 'column', el('h2', '', status, el('span', 'count', String(tasks.length))), list)
  col.dataset.status = status
  col.addEventListener('dragover', (e) => {
    if (!state.dragging) return
    e.preventDefault()
    col.classList.add('drop-target')
  })
  col.addEventListener('dragleave', (e) => {
    if (!col.contains(e.relatedTarget as Node | null)) col.classList.remove('drop-target')
  })
  col.addEventListener('drop', (e) => {
    e.preventDefault()
    col.classList.remove('drop-target')
    const drag = state.dragging
    state.dragging = null
    if (!drag) return
    const action = dropAction(drag, { column: status })
    if (action.kind === 'move') void moveTask(drag.id, drag.from, action.to)
  })
  return col
}

// card shows only the title; the full Markdown body opens in the detail dialog.
function card(t: Task): HTMLElement {
  const node = el('article', 'card')
  node.draggable = true
  node.dataset.id = String(t.id)
  node.addEventListener('dragstart', (e) => {
    state.dragging = { id: t.id, from: t.status }
    e.dataTransfer?.setData('text/plain', String(t.id))
    node.classList.add('dragging')
  })
  node.addEventListener('dragend', () => {
    state.dragging = null
    node.classList.remove('dragging')
  })
  // Dropping on another card merges into it; the column below does not see these events.
  node.dataset.mergeLabel = `Merge into #${t.id}`
  node.addEventListener('dragover', (e) => {
    if (!state.dragging || state.dragging.id === t.id) return
    e.preventDefault()
    e.stopPropagation()
    node.closest('.column')?.classList.remove('drop-target')
    node.classList.add('merge-target')
  })
  node.addEventListener('dragleave', (e) => {
    if (!node.contains(e.relatedTarget as Node | null)) node.classList.remove('merge-target')
  })
  node.addEventListener('drop', (e) => {
    const drag = state.dragging
    if (!drag) return
    e.preventDefault()
    e.stopPropagation()
    node.classList.remove('merge-target')
    node.closest('.column')?.classList.remove('drop-target')
    state.dragging = null
    const action = dropAction(drag, { card: t.id, column: t.status })
    if (action.kind === 'merge') void mergeTask(drag.id, t)
  })

  const title = button(t.title, 'card-title', () => openDetail(t.id), `Open #${t.id}: ${t.title}`)
  const del = button('×', 'card-delete', () => void removeTask(t), `Delete #${t.id}`)
  node.append(el('div', 'card-head', el('span', 'card-id', `#${t.id}`, ...badges(t)), del), title)
  const hue = cardHue(t)
  if (hue) node.dataset.hue = hue
  if (t.kind === 'epic') node.classList.add('epic')
  const epic = epicOf(t)
  if (epic) {
    node.classList.add('in-epic')
    node.append(el('span', 'card-epic', `#${epic.id} ${epic.title}`))
  } else if (t.epicId !== null) {
    node.classList.add('in-epic')
    node.append(el('span', 'card-epic', `#${t.epicId}`))
  }
  return node
}

// badges are the small labels after a card's id: EPIC, and a priority other than normal.
function badges(t: Task): HTMLElement[] {
  const out: HTMLElement[] = []
  if (t.kind === 'epic') out.push(el('span', 'badge badge-epic', 'EPIC'))
  if (t.priority !== 'normal') out.push(el('span', `badge badge-${t.priority}`, t.priority))
  return out
}

function epicOf(t: Task): Task | undefined {
  return t.epicId === null ? undefined : state.board?.tasks.find((x) => x.id === t.epicId)
}

function epics(): { id: number; title: string }[] {
  return (state.board?.tasks ?? []).filter((t) => t.kind === 'epic')
}

function renderDetail() {
  if (state.openId === null) return
  const t = state.board?.tasks.find((x) => x.id === state.openId)
  if (!t) {
    closeDetail()
    toast('The task was deleted.')
    return
  }
  const meta = el('span', 'card-id', `#${t.id} · ${t.status}`, ...badges(t))
  meta.id = 'detail-meta'
  const epic = epicOf(t)
  if (epic) meta.append(button(`#${epic.id} ${epic.title}`, 'detail-epic', () => openDetail(epic.id)))
  const head = el('div', 'detail-head', meta,
    el('div', 'edit-actions', button('Edit', '', () => editTask(t)), button('Close', '', closeDetail)))
  ui.detail.replaceChildren(head, markdown(t.body), ...epicTasks(t))
  const hue = cardHue(t)
  if (hue) ui.detail.dataset.hue = hue
  else delete ui.detail.dataset.hue
  if (t.kind === 'epic') ui.detail.classList.add('epic')
  else ui.detail.classList.remove('epic')
  if (!ui.detail.open) ui.detail.showModal()
}

// epicTasks lists the tasks of an epic in its detail dialog.
function epicTasks(t: Task): HTMLElement[] {
  if (t.kind !== 'epic') return []
  const tasks = (state.board?.tasks ?? []).filter((x) => x.epicId === t.id)
  const items = tasks.map((x) =>
    el('li', '', button(`#${x.id} ${x.title}`, 'detail-epic-task', () => openDetail(x.id)), el('span', 'card-id', ` ${x.status}`)))
  return [el('h3', 'detail-section', `Tasks in this epic (${tasks.length})`),
    items.length ? el('ul', 'detail-epic-tasks', ...items) : el('p', 'empty', 'No tasks yet.')]
}

// edit opens the full-screen Markdown editor; write stores its values and returns once they are saved.
function edit(heading: string, body: string, submitLabel: string, fields: EditorFields,
  write: (values: EditorValues) => Promise<unknown>) {
  state.editing = true
  openEditor(ui.editor, {
    heading,
    body,
    submitLabel,
    fields,
    save: async (values) => {
      try {
        await write(values)
      } catch (err) {
        toast(errorMessage(err)) // the editor stays open, so the text is not lost
        return false
      }
      boardKey = '' // re-render the detail dialog even if the saved body matches the last poll
      await refresh()
      return true
    },
    onClose: () => {
      state.editing = false
    },
  })
}

function addTask() {
  const fields = { kind: epics().length ? 'task' as const : 'epic' as const, priority: 'normal', priorities: state.board?.priorities ?? [], epic: null, epics: epics() }
  edit('New task', '', 'Add to backlog', fields, (v) =>
    api.create(state.repo, { body: v.body, kind: v.kind, priority: v.priority, ...(v.kind === 'epic' ? {} : { epic: v.epic! }) }))
}

function editTask(t: Task) {
  // An epic cannot belong to an epic, so its epic select stays empty.
  const fields = { cardKind: t.kind, priority: t.priority, priorities: state.board?.priorities ?? [], epic: t.epicId,
    epics: t.kind === 'epic' ? [] : epics().filter((e) => e.id !== t.id) }
  edit(`Edit #${t.id}`, t.body, 'Save', fields, (v) =>
    api.patch(state.repo, t.id, { body: v.body, priority: v.priority, ...(t.kind === 'epic' ? {} : { epic: v.epic! }) }))
}

function openDetail(id: number) {
  state.openId = id
  renderDetail()
}

function closeDetail() {
  state.openId = null
  if (ui.detail.open) ui.detail.close()
}

async function moveTask(id: number, from: Status, to: Status) {
  try {
    await api.patch(state.repo, id, { status: to, from })
  } catch (err) {
    if (err instanceof ApiError && err.status === 409) {
      toast(`#${id} was already moved to ${err.current}; the board is refreshed.`)
    } else {
      toast(errorMessage(err))
    }
  }
  await refresh()
}

async function mergeTask(sourceId: number, target: Task) {
  const source = state.board?.tasks.find((x) => x.id === sourceId)
  const question = `Merge #${sourceId} "${source?.title ?? ''}" into #${target.id} "${target.title}"? ` +
    `#${sourceId} will be deleted and its content added to #${target.id}.`
  if (!confirm(question)) return
  let merged: Task
  try {
    merged = await api.merge(state.repo, target.id, sourceId)
  } catch (err) {
    if (err instanceof ApiError && err.status === 409) {
      toast(`#${sourceId} is in ${err.current}; move it out of ${err.current} before merging.`)
    } else {
      toast(errorMessage(err))
    }
    await refresh()
    return
  }
  await refresh()
  editTask(merged) // tidy the combined body
}

async function removeTask(t: Task) {
  if (!confirm(`Delete #${t.id} "${t.title}"? This cannot be undone.`)) return
  try {
    await api.remove(state.repo, t.id)
  } catch (err) {
    toast(errorMessage(err))
  }
  await refresh()
}

ui.select.addEventListener('change', () => {
  setRepo(ui.select.value)
  void refresh()
})

ui.addButton.addEventListener('click', addTask)

ui.detail.addEventListener('close', () => {
  state.openId = null
})
ui.detail.addEventListener('click', (e) => {
  if (e.target === ui.detail) closeDetail() // backdrop click
})

// Poll so tasks written by agents appear; pause while dragging, editing or hidden.
setInterval(() => {
  if (!document.hidden && !state.dragging && !state.editing) void refresh()
}, POLL_MS)
document.addEventListener('visibilitychange', () => {
  if (!document.hidden) void refresh()
})

void refresh()
