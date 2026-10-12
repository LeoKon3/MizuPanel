export class APIError extends Error {
  constructor(public status: number, message = `Request failed: ${status}`) {
    super(message)
  }
}

type UnauthorizedHandler = () => void

let unauthorizedHandler: UnauthorizedHandler | undefined

export function setUnauthorizedHandler(handler?: UnauthorizedHandler) {
  unauthorizedHandler = handler
}

export async function responseErrorMessage(response: Response): Promise<string> {
  try {
    const body = await response.json() as { error?: string }
    return body.error || `Request failed: ${response.status}`
  } catch {
    return `Request failed: ${response.status}`
  }
}

export async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const response = init === undefined ? await fetch(path) : await fetch(path, init)
  if (!response.ok) {
    const error = new APIError(response.status, await responseErrorMessage(response))
    if (response.status === 401) unauthorizedHandler?.()
    throw error
  }
  return response.json() as Promise<T>
}

export async function requestVoid(path: string, init?: RequestInit): Promise<void> {
  const response = init === undefined ? await fetch(path) : await fetch(path, init)
  if (!response.ok) {
    const error = new APIError(response.status, await responseErrorMessage(response))
    if (response.status === 401) unauthorizedHandler?.()
    throw error
  }
}
