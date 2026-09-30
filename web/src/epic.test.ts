import { describe, expect, it } from 'vitest'
import { EPIC_HUES, cardHue } from './epic'

describe('cardHue', () => {
  it('gives each epic a stable hue from the palette, cycling through all of them', () => {
    const hues = [6, 7, 8, 9, 10, 11].map((id) => cardHue({ id, kind: 'epic', epicId: null }))
    expect(new Set(hues).size).toBe(EPIC_HUES.length)
    expect(cardHue({ id: 6, kind: 'epic', epicId: null })).toBe(cardHue({ id: 12, kind: 'epic', epicId: null }))
    expect(cardHue({ id: 6, kind: 'epic', epicId: null })).toBe('green')
  })

  it("colors a task with its epic's hue and leaves other tasks plain", () => {
    expect(cardHue({ id: 40, kind: 'task', epicId: 6 })).toBe(cardHue({ id: 6, kind: 'epic', epicId: null }))
    expect(cardHue({ id: 40, kind: 'task', epicId: null })).toBeNull()
  })
})
