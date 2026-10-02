// @vitest-environment jsdom
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import { api, ApiError, type Board, type Task } from './api'

vi.mock('./api', async (original) => ({
  ...await original<typeof import('./api')>(),
  api: { repos: vi.fn(), board: vi.fn(), create: vi.fn(), patch: vi.fn(), merge: vi.fn(), remove: vi.fn() },
}))

const makeTask = (id: number, repo: string, title: string): Task => ({
  id, repo, title, body: `# ${title}`, status: 'todo', kind: 'epic', priority: 'normal', epicId: null,
  createdAt: '', updatedAt: '', revision: 1,
})
const board = (tasks: Task[]): Board => ({ statuses: ['backlog', 'todo', 'doing', 'done'], priorities: ['normal', 'high'], tasks })
const a = makeTask(1, '/work/a', 'A target')
const source = makeTask(2, '/work/a', 'A source')
const b = makeTask(3, '/work/b', 'B task')
const boardA = board([a, source])
const boardB = board([b])
const deferred = <T>() => {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((done) => { resolve = done })
  return { promise, resolve }
}
const flush = () => vi.advanceTimersByTimeAsync(0)
const start = async () => { await import('./main'); await flush() }
const byId = <T extends HTMLElement>(id: string) => document.getElementById(id) as T
const button = (root: ParentNode, text: string) => [...root.querySelectorAll('button')].find((node) => node.textContent === text)!
const editor = () => byId<HTMLDialogElement>('editor')
const edit = () => {
  document.querySelector<HTMLButtonElement>('[data-id="1"] .card-title')!.click()
  button(byId('detail'), 'Edit').click()
}
const switchRepo = (repo: string) => {
  byId<HTMLSelectElement>('repo').value = repo
  byId('repo').dispatchEvent(new Event('change'))
}
const startMerge = () => {
  document.querySelector('[data-id="2"]')!.dispatchEvent(new Event('dragstart'))
  document.querySelector('[data-id="1"]')!.dispatchEvent(new Event('drop', { bubbles: true }))
}
let removeDocumentListeners: (() => void)[] = []

beforeAll(() => {
  HTMLDialogElement.prototype.showModal = function () { this.setAttribute('open', '') }
  HTMLDialogElement.prototype.close = function () {
    if (!this.open) return
    this.removeAttribute('open')
    this.dispatchEvent(new Event('close'))
  }
})
beforeEach(async () => {
  vi.resetModules()
  vi.clearAllMocks()
  vi.useFakeTimers()
  history.replaceState(null, '', '/work/a')
  Object.defineProperty(document, 'hidden', { configurable: true, value: false })
  document.body.innerHTML = '<select id="repo"></select><button id="add-task">Add to backlog</button><p id="empty"></p><div id="columns"></div><dialog id="detail"></dialog><dialog id="editor"></dialog><div id="toast"></div>'
  const listen = document.addEventListener.bind(document)
  vi.spyOn(document, 'addEventListener').mockImplementation((type, listener, options) => {
    listen(type, listener, options)
    removeDocumentListeners.push(() => document.removeEventListener(type, listener, options))
  })
  vi.spyOn(window, 'confirm').mockReturnValue(true)
  vi.mocked(api.repos).mockResolvedValue([{ path: '/work/a', name: 'a', count: 2 }, { path: '/work/b', name: 'b', count: 1 }])
  vi.mocked(api.board).mockImplementation(async (repo) => repo === '/work/a' ? boardA : boardB)
  vi.mocked(api.patch).mockResolvedValue(a)
})
afterEach(() => {
  removeDocumentListeners.forEach((remove) => remove())
  removeDocumentListeners = []
  vi.clearAllTimers()
  vi.useRealTimers()
  vi.restoreAllMocks()
})

describe('board orchestration', () => {
  beforeEach(start)

  it('ignores an old board response after selecting another repo', async () => {
    const old = deferred<Board>()
    vi.mocked(api.board).mockImplementation((repo) => repo === '/work/a' ? old.promise : Promise.resolve(boardB))
    document.dispatchEvent(new Event('visibilitychange'))
    await flush()
    switchRepo('/work/b')
    await flush()
    old.resolve(boardA)
    await flush()
    expect(byId<HTMLSelectElement>('repo').value).toBe('/work/b')
    expect(byId('columns').textContent).toContain('B task')
    expect(byId('columns').textContent).not.toContain('A target')
  })

  it('removes the previous repo cards while the new board loads', async () => {
    vi.mocked(api.board).mockReturnValue(deferred<Board>().promise)
    switchRepo('/work/b')
    expect(byId('columns').querySelector('.card')).toBeNull()
    expect(byId<HTMLButtonElement>('add-task').disabled).toBe(true)
  })

  it('preserves a draft opened while a merge is pending', async () => {
    const merged = deferred<Task>()
    vi.mocked(api.merge).mockReturnValue(merged.promise)
    startMerge()
    byId<HTMLButtonElement>('add-task').click()
    editor().querySelector('textarea')!.value = '# Important draft'
    merged.resolve({ ...a, body: '# Combined' })
    await flush()
    expect(editor().querySelector('h2')!.textContent).toBe('New task')
    expect(editor().querySelector('textarea')!.value).toBe('# Important draft')
  })

  it('does not reopen a merged target that the refreshed board says was deleted', async () => {
    vi.mocked(api.merge).mockResolvedValue({ ...a, revision: 2 })
    vi.mocked(api.board).mockResolvedValue(board([]))
    startMerge()
    await flush()
    expect(editor().open).toBe(false)
  })

  it('sends only changed fields with the version that the editor opened', async () => {
    edit()
    const priority = editor().querySelector<HTMLSelectElement>('[aria-label="Priority"]')!
    priority.value = 'high'
    button(editor(), 'Save').click()
    await flush()
    expect(api.patch).toHaveBeenCalledWith('/work/a', 1, { priority: 'high', expectedRevision: 1 })
  })

  it('preserves the draft and explains a version conflict inside the editor without retrying', async () => {
    vi.mocked(api.patch).mockRejectedValue(new ApiError('Task changed since it was opened.', 409))
    edit()
    editor().querySelector('textarea')!.value = '# My unsaved changes'
    button(editor(), 'Save').click()
    await flush()
    expect(editor().open).toBe(true)
    expect(editor().querySelector('textarea')!.value).toBe('# My unsaved changes')
    expect(editor().querySelector('[role="alert"]')?.textContent).toMatch(/changed.*draft/i)
    expect(api.patch).toHaveBeenCalledOnce()
  })

  it('moves a task using the accessible status selector', async () => {
    document.querySelector<HTMLButtonElement>('[data-id="1"] .card-title')!.click()
    const select = byId('detail').querySelector<HTMLSelectElement>('[aria-label="Status"]')
    expect(select).not.toBeNull()
    select!.value = 'doing'
    select!.dispatchEvent(new Event('change'))
    await flush()
    expect(api.patch).toHaveBeenCalledWith('/work/a', 1, { status: 'doing', from: 'todo' })
  })

  it('merges through labeled controls and the same confirmation used by dragging', async () => {
    vi.mocked(api.merge).mockResolvedValue(a)
    document.querySelector<HTMLButtonElement>('[data-id="2"] .card-title')!.click()
    const select = byId('detail').querySelector<HTMLSelectElement>('[aria-label="Merge into"]')
    expect(select).not.toBeNull()
    select!.value = '1'
    select!.dispatchEvent(new Event('change'))
    button(byId('detail'), 'Merge').click()
    await flush()
    expect(window.confirm).toHaveBeenCalledWith(expect.stringContaining('#2 will be deleted'))
    expect(api.merge).toHaveBeenCalledWith('/work/a', 1, 2)
  })

  it('does not submit the same merge again while its request is pending', async () => {
    const pending = deferred<Task>()
    vi.mocked(api.merge).mockReturnValue(pending.promise)
    startMerge()
    startMerge()
    expect(api.merge).toHaveBeenCalledOnce()
    pending.resolve(a)
    await flush()
  })
})

it('lets a slow initial board request finish instead of superseding it on every poll', async () => {
  const initial = deferred<Board>()
  vi.mocked(api.board).mockReturnValue(initial.promise)
  await start()
  await vi.advanceTimersByTimeAsync(6000)
  expect(api.board).toHaveBeenCalledOnce()
  initial.resolve(boardA)
  await flush()
  expect(byId('columns').textContent).toContain('A target')
})
