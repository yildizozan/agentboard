// @vitest-environment jsdom
import { beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import { openEditor } from './editor'

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

function open(save = vi.fn(async () => true), onClose = vi.fn()) {
  openEditor(dialog, { heading: 'New task', body: '# Draft\n\nmore', submitLabel: 'Add', save, onClose })
  return { save, onClose }
}

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
    expect(save).toHaveBeenCalledWith('# Changed')
    expect(dialog.open).toBe(false)
    expect(onClose).toHaveBeenCalledOnce()
  })

  it('stays open with the text when the save fails', async () => {
    const { onClose } = open(vi.fn(async () => false))
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
})
