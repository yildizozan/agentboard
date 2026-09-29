import './style.css'
import { api, ApiError, type Board, type Repo, type Status, type Task } from './api'
import { openEditor } from './editor'
import { renderMarkdown } from './markdown'

const POLL_MS = 2000
const TOAST_MS = 4000

const state = {
  repos: [] as Repo[],
  repo: new URLSearchParams(location.search).get('repo') ?? '',
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
  const url = new URL(location.href)
  url.searchParams.set('repo', path)
  history.replaceState(null, '', url)
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
    if (drag && drag.from !== status) void moveTask(drag.id, drag.from, status)
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

  const title = button(t.title, 'card-title', () => openDetail(t.id), `Open #${t.id}: ${t.title}`)
  const del = button('×', 'card-delete', () => void removeTask(t), `Delete #${t.id}`)
  node.append(el('div', 'card-head', el('span', 'card-id', `#${t.id}`), del), title)
  return node
}

function renderDetail() {
  if (state.openId === null) return
  const t = state.board?.tasks.find((x) => x.id === state.openId)
  if (!t) {
    closeDetail()
    toast('The task was deleted.')
    return
  }
  const meta = el('span', 'card-id', `#${t.id} · ${t.status}`)
  meta.id = 'detail-meta'
  const head = el('div', 'detail-head', meta,
    el('div', 'edit-actions', button('Edit', '', () => editTask(t)), button('Close', '', closeDetail)))
  ui.detail.replaceChildren(head, markdown(t.body))
  if (!ui.detail.open) ui.detail.showModal()
}

// edit opens the full-screen Markdown editor; write stores its body and returns once it is saved.
function edit(heading: string, body: string, submitLabel: string, write: (body: string) => Promise<unknown>) {
  state.editing = true
  openEditor(ui.editor, {
    heading,
    body,
    submitLabel,
    save: async (text) => {
      try {
        await write(text)
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
  edit('New task', '', 'Add to backlog', (body) => api.create(state.repo, body))
}

function editTask(t: Task) {
  edit(`Edit #${t.id}`, t.body, 'Save', (body) => api.patch(state.repo, t.id, { body }))
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
