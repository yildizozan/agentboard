// @vitest-environment jsdom
import { beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import { openEditor, type EditorFields, type EditorValues } from './editor'

// jsdom has no modal dialogs; this stands in for the browser's open/close behavior.
beforeAll(() => {
  HTMLDialogElement.prototype.showModal = function () {
    this.setAttribute('open', '')
  }
  HTMLDialogElement.prototype.close = function () {
    if (!this.hasAttribute('open')) return
    this.removeAttribute('open')
    this.dispatchEvent(new Event('close'))
  }
})

let dialog: HTMLDialogElement
beforeEach(() => {
  document.body.replaceChildren()
  dialog = document.createElement('dialog')
  document.body.append(dialog)
})

const textarea = () => dialog.querySelector('textarea')!
const buttonNamed = (label: string) =>
  [...dialog.querySelectorAll('button')].find((b) => b.textContent === label)!
const flush = () => new Promise((resolve) => setTimeout(resolve))

function open(save = vi.fn(async (_: EditorValues) => true), onClose = vi.fn(), fields?: EditorFields) {
  openEditor(dialog, { heading: 'New task', body: '# Draft\n\nmore', submitLabel: 'Add', save, onClose, fields })
  return { save, onClose }
}

const select = (label: string) => dialog.querySelector<HTMLSelectElement>(`select[aria-label="${label}"]`)
const epics = [{ id: 3, title: 'Auth rewrite' }, { id: 9, title: 'Billing' }]

describe('openEditor', () => {
  it('opens modally with the heading and the body ready to edit from the start', () => {
    open()
    expect(dialog.open).toBe(true)
    expect(dialog.textContent).toContain('New task')
    expect(textarea().value).toBe('# Draft\n\nmore')
    expect(document.activeElement).toBe(textarea())
    expect(textarea().selectionStart).toBe(0)
  })

  it('saves the edited body and closes once the save succeeds', async () => {
    const { save, onClose } = open()
    textarea().value = '# Changed'
    buttonNamed('Add').click()
    await flush()
    expect(save).toHaveBeenCalledWith({ body: '# Changed' })
    expect(dialog.open).toBe(false)
    expect(onClose).toHaveBeenCalledOnce()
  })

  it('stays open with the text when the save fails', async () => {
    const { onClose } = open(vi.fn(async (_: EditorValues) => false))
    textarea().value = '# Keep me'
    buttonNamed('Add').click()
    await flush()
    expect(dialog.open).toBe(true)
    expect(textarea().value).toBe('# Keep me')
    expect(onClose).not.toHaveBeenCalled()
  })

  it('submits with Cmd+Enter and Ctrl+Enter', async () => {
    for (const mod of [{ metaKey: true }, { ctrlKey: true }]) {
      const { save } = open()
      textarea().dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true, ...mod }))
      await flush()
      expect(save).toHaveBeenCalledOnce()
    }
  })

  it('closes without saving on Cancel', () => {
    const { save, onClose } = open()
    buttonNamed('Cancel').click()
    expect(dialog.open).toBe(false)
    expect(save).not.toHaveBeenCalled()
    expect(onClose).toHaveBeenCalledOnce()
  })

  it('reports a close by Esc, which the browser performs itself', () => {
    const { save, onClose } = open()
    dialog.close() // what the browser does after its cancel event
    expect(save).not.toHaveBeenCalled()
    expect(onClose).toHaveBeenCalledOnce()
  })

  it('does not report later closes to an earlier opening of the same dialog', () => {
    const first = open()
    buttonNamed('Cancel').click()
    const second = open()
    buttonNamed('Cancel').click()
    expect(first.onClose).toHaveBeenCalledOnce()
    expect(second.onClose).toHaveBeenCalledOnce()
  })

  it('shows no field selects without fields', () => {
    open()
    expect(dialog.querySelectorAll('select')).toHaveLength(0)
  })

  it('offers priority and epic with the current values and saves the chosen ones', async () => {
    const { save } = open(undefined, undefined, { priority: 'normal', priorities: ['low', 'normal', 'high'], epic: 3, epics })
    expect(select('Kind')).toBeNull() // the kind is chosen only for new cards
    expect([...select('Priority')!.options].map((o) => o.value)).toEqual(['low', 'normal', 'high'])
    expect(select('Priority')!.value).toBe('normal')
    expect([...select('Epic')!.options].map((o) => o.textContent)).toEqual(['No epic', '#3 Auth rewrite', '#9 Billing'])
    expect(select('Epic')!.value).toBe('3')

    select('Priority')!.value = 'high'
    select('Epic')!.value = '9'
    buttonNamed('Add').click()
    await flush()
    expect(save).toHaveBeenCalledWith({ body: '# Draft\n\nmore', priority: 'high', epic: 9 })
  })

  it('saves no epic as null', async () => {
    const { save } = open(undefined, undefined, { priority: 'low', priorities: ['low', 'normal', 'high'], epic: null, epics })
    expect(select('Epic')!.value).toBe('')
    buttonNamed('Add').click()
    await flush()
    expect(save).toHaveBeenCalledWith({ body: '# Draft\n\nmore', priority: 'low', epic: null })
  })

  it('lets a new card be an epic, which cannot belong to an epic', async () => {
    const { save } = open(undefined, undefined, { kind: 'task', priority: 'normal', priorities: ['low', 'normal', 'high'], epic: 3, epics })
    expect(select('Kind')!.value).toBe('task')
    select('Kind')!.value = 'epic'
    select('Kind')!.dispatchEvent(new Event('change'))
    expect(select('Epic')!.disabled).toBe(true)
    buttonNamed('Add').click()
    await flush()
    expect(save).toHaveBeenCalledWith({ body: '# Draft\n\nmore', kind: 'epic', priority: 'normal', epic: null })
  })

})
