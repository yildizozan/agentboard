import { describe, expect, it } from 'vitest'
import { dropAction } from './drop'

const dragged = { id: 7, from: 'todo' }

describe('dropAction', () => {
  it('merges into another card it is dropped on, in any column', () => {
    expect(dropAction(dragged, { card: 3, column: 'doing' })).toEqual({ kind: 'merge', target: 3 })
    expect(dropAction(dragged, { card: 3, column: 'todo' })).toEqual({ kind: 'merge', target: 3 })
  })

  it('does nothing when dropped on itself', () => {
    expect(dropAction(dragged, { card: 7, column: 'todo' })).toEqual({ kind: 'none' })
  })

  it('moves when dropped on another column outside a card', () => {
    expect(dropAction(dragged, { column: 'done' })).toEqual({ kind: 'move', to: 'done' })
  })

  it('does nothing when dropped back on its own column', () => {
    expect(dropAction(dragged, { column: 'todo' })).toEqual({ kind: 'none' })
  })
})
