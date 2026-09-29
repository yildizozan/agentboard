// Typed client for the agentboard JSON API (internal/httpapi).

export type Status = string

export interface Task {
  id: number
  repo: string
  title: string // derived by the server from the body's "# " first line
  body: string // Markdown
  status: Status
  createdAt: string
  updatedAt: string
}

export interface Repo {
  path: string
  name: string
  count: number
}

export interface Board {
  // Column order comes from the server; the UI never hard-codes statuses.
  statuses: Status[]
  tasks: Task[]
}

export interface Patch {
  body?: string
  status?: Status
  from?: Status
}

export class ApiError extends Error {
  status: number
  current?: Status

  constructor(message: string, status: number, current?: Status) {
    super(message)
    this.status = status
    this.current = current
  }
}

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  const res = await fetch(path, {
    method,
    headers: body === undefined ? undefined : { 'Content-Type': 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body),
  })
  if (!res.ok) {
    let data: { error?: string; current?: Status } = {}
    try {
      data = await res.json()
    } catch {
      // non-JSON error body; fall back to the status text
    }
    throw new ApiError(data.error ?? res.statusText, res.status, data.current)
  }
  return (res.status === 204 ? undefined : await res.json()) as T
}

const repoQuery = (repo: string) => `?repo=${encodeURIComponent(repo)}`

export const api = {
  repos: () => request<Repo[]>('GET', '/api/repos'),
  board: (repo: string) => request<Board>('GET', `/api/tasks${repoQuery(repo)}`),
  create: (repo: string, body: string) => request<Task>('POST', `/api/tasks${repoQuery(repo)}`, { body }),
  patch: (repo: string, id: number, patch: Patch) =>
    request<Task>('PATCH', `/api/tasks/${id}${repoQuery(repo)}`, patch),
  remove: (repo: string, id: number) => request<void>('DELETE', `/api/tasks/${id}${repoQuery(repo)}`),
}
