import { describe, expect, it } from 'vitest'
import { boardPath, repoFromPath } from './route'

describe('board routes', () => {
  it('uses the repo path as the page path', () => {
    expect(boardPath('/Users/ozan.yildiz/projects/agent-todo')).toBe('/Users/ozan.yildiz/projects/agent-todo')
    expect(repoFromPath('/Users/ozan.yildiz/projects/agent-todo')).toBe('/Users/ozan.yildiz/projects/agent-todo')
  })

  it('escapes characters that would end or change the path, and reads them back', () => {
    const repo = '/work/a b#c?d%e'
    expect(boardPath(repo)).toBe('/work/a%20b%23c%3Fd%25e')
    expect(repoFromPath(boardPath(repo))).toBe(repo)
  })

  it('round-trips Windows repo keys', () => {
    const repo = 'C:\\Users\\x'
    expect(boardPath(repo)).toBe('/C%3A%5CUsers%5Cx')
    expect(repoFromPath(boardPath(repo))).toBe(repo)
    expect(repoFromPath('/C:%5CUsers%5Cx')).toBe(repo) // as printed by agentboard board
  })

  it('reads no repo from the root and ignores a trailing slash', () => {
    expect(repoFromPath('/')).toBe('')
    expect(repoFromPath('/work/a/')).toBe('/work/a')
  })
})
