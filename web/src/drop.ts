import type { Status } from './api'

export type DropAction = { kind: 'move'; to: Status } | { kind: 'merge'; target: number } | { kind: 'none' }

// dropAction decides what dropping a dragged card does: on another card it merges into
// that card, elsewhere in a column it moves the card there.
export function dropAction(
  dragged: { id: number; from: Status },
  over: { card?: number; column: Status },
): DropAction {
  if (over.card !== undefined) {
    return over.card === dragged.id ? { kind: 'none' } : { kind: 'merge', target: over.card }
  }
  return over.column === dragged.from ? { kind: 'none' } : { kind: 'move', to: over.column }
}
