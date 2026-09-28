import { ApiError } from './client'

// User-facing copy is owned by the dashboard; the backend only returns the
// stable error_code so wording can change without a server deploy.
const AUTH_ERROR_MESSAGES: Record<string, string> = {
  username_not_found: '用户名不存在',
  wrong_password: '密码错误',
  username_taken: '该用户名已被注册',
  username_length: '用户名长度需为 3-64 个字符',
  username_whitespace: '用户名不能包含空格',
  password_length: '密码长度需为 8-72 个字符',
  registration_disabled: '当前未开放注册',
  unauthenticated: '登录状态已失效，请重新登录',
}

// authErrorMessage maps a backend error_code to localized copy, falling back to
// the server message and then a caller-supplied default.
export function authErrorMessage(error: unknown, fallback: string): string {
  if (error instanceof ApiError) {
    const localized = error.code ? AUTH_ERROR_MESSAGES[error.code] : undefined
    if (localized) return localized
    return error.message || fallback
  }
  return error instanceof Error ? error.message || fallback : fallback
}
