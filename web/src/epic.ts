// Every epic gets a hue from this palette by its id; its tasks use a light tone of the same hue.
export const EPIC_HUES = ['green', 'blue', 'orange', 'purple', 'pink', 'teal'] as const

export type Hue = (typeof EPIC_HUES)[number]

// cardHue returns the hue of an epic or of a task in an epic, and null for other tasks.
export function cardHue(t: { id: number; kind: string; epicId: number | null }): Hue | null {
  const epic = t.kind === 'epic' ? t.id : t.epicId
  return epic === null ? null : EPIC_HUES[epic % EPIC_HUES.length]
}
