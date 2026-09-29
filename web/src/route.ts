// The page path is the board's repo path, e.g. /Users/me/project for the repo /Users/me/project.

// Windows repo keys (C:\src\app) sit after the leading slash of the page path.
const windowsKey = /^\/[A-Za-z]:\\/

export function boardPath(repo: string): string {
  const path = repo.startsWith('/') ? repo : `/${repo}`
  return path.split('/').map(encodeURIComponent).join('/')
}

export function repoFromPath(pathname: string): string {
  let repo = decodeURIComponent(pathname)
  if (repo.length > 1 && repo.endsWith('/')) repo = repo.slice(0, -1)
  if (repo === '/') return ''
  return windowsKey.test(repo) ? repo.slice(1) : repo
}
