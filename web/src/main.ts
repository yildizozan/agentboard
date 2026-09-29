import './style.css'
import { api, ApiError, type Board, type Repo, type Status, type Task } from './api'

const POLL_MS = 2000
const TOAST_MS = 4000

const state = {
  repos: [] as Repo[],
  repo: new URLSearchParams(location.search).get('repo') ?? '',
  board: null as Board | null,
  dragging: null as { id: number; from: Status } | null,
  editingId: null as number | null,
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
  addForm: byId<HTMLFormElement>('add-form'),
  newTitle: byId<HTMLInputElement>('new-title'),
  newDesc: byId<HTMLInputElement>('new-desc'),
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

async function refresh() {
  try {
    state.repos = await api.repos()
    if (!state.repo && state.repos.length > 0) setRepo(state.repos[0].path)
    state.board = state.repo ? await api.board(state.repo) : null
    render()
  } catch (err) {
    toast(errorMessage(err))
  }
}

function setRepo(path: string) {
  state.repo = path
  state.editingId = null
  const url = new URL(location.href)
  url.searchParams.set('repo', path)
  history.replaceState(null, '', url)
}

function render() {
  renderSelect()
  ui.addForm.querySelector('button')!.disabled = !state.repo
  if (!state.board) {
    ui.columns.replaceChildren()
    ui.empty.hidden = false
    ui.empty.textContent = 'No boards yet. A board appears when an agent or the CLI adds its first task.'
    return
  }
  ui.empty.hidden = true
  const { statuses, tasks } = state.board
  ui.columns.replaceChildren(...statuses.map((s) => column(s, tasks.filter((t) => t.status === s))))
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
    if (drag && drag.from !== status) void moveTask(drag.id, drag.from, status)
  })
  return col
}

function card(t: Task): HTMLElement {
  if (t.id === state.editingId) return editCard(t)
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

  const title = button(t.title, 'card-title', () => startEdit(t.id), `Edit #${t.id}: ${t.title}`)
  const del = button('×', 'card-delete', () => void removeTask(t), `Delete #${t.id}`)
  node.append(el('div', 'card-head', el('span', 'card-id', `#${t.id}`), del), title)
  const firstLine = t.description.split('\n')[0]
  if (firstLine) node.append(el('p', 'card-desc', firstLine))
  return node
}

function editCard(t: Task): HTMLElement {
  const title = el('input', 'edit-title')
  title.value = t.title
  title.setAttribute('aria-label', 'Title')
  const desc = el('textarea', 'edit-desc')
  desc.value = t.description
  desc.rows = 3
  desc.setAttribute('aria-label', 'Description')

  const save = el('button', 'primary', 'Save')
  save.type = 'submit'
  const form = el('form', 'card editing', el('span', 'card-id', `#${t.id}`), title, desc,
    el('div', 'edit-actions', save, button('Cancel', '', stopEdit)))
  form.addEventListener('submit', (e) => {
    e.preventDefault()
    void saveTask(t.id, title.value, desc.value)
  })
  form.addEventListener('keydown', (e) => {
    if (e.key === 'Escape') stopEdit()
  })
  queueMicrotask(() => title.focus())
  return form
}

function startEdit(id: number) {
  state.editingId = id
  render()
}

function stopEdit() {
  state.editingId = null
  render()
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

async function saveTask(id: number, title: string, description: string) {
  try {
    await api.patch(state.repo, id, { title, description })
    state.editingId = null
    await refresh()
  } catch (err) {
    toast(errorMessage(err)) // stay in edit mode so the input is not lost
  }
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

ui.addForm.addEventListener('submit', async (e) => {
  e.preventDefault()
  try {
    await api.create(state.repo, ui.newTitle.value, ui.newDesc.value)
    ui.addForm.reset()
    ui.newTitle.focus()
  } catch (err) {
    toast(errorMessage(err))
  }
  await refresh()
})

// Poll so tasks written by agents appear; pause while dragging, editing or hidden.
setInterval(() => {
  if (!document.hidden && !state.dragging && state.editingId === null) void refresh()
}, POLL_MS)
document.addEventListener('visibilitychange', () => {
  if (!document.hidden) void refresh()
})

void refresh()
