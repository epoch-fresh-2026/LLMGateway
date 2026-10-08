import '@testing-library/jest-dom/vitest'
import { afterAll, afterEach, beforeEach, vi } from 'vitest'
import { cleanup } from '@testing-library/react'

// Node's Request requires an absolute URL; browsers resolve same-origin paths.
const NativeRequest = globalThis.Request
globalThis.Request = class extends NativeRequest {
  constructor(input: RequestInfo | URL, init?: RequestInit) {
    super(typeof input === 'string' ? new URL(input, window.location.origin) : input, init)
  }
}
class ResizeObserverDouble {
  constructor(private callback: ResizeObserverCallback) {}
  observe(target: Element) {
    this.callback([{ target, contentRect: { width: 800, height: 280 } } as ResizeObserverEntry], this as unknown as ResizeObserver)
  }
  unobserve() {}
  disconnect() {}
}
globalThis.ResizeObserver = ResizeObserverDouble as unknown as typeof ResizeObserver
Object.defineProperty(HTMLElement.prototype, 'getBoundingClientRect', { configurable: true, value: () => ({ width: 800, height: 280, top: 0, left: 0, right: 800, bottom: 280, x: 0, y: 0, toJSON() {} }) })
Object.defineProperty(HTMLElement.prototype, 'clientWidth', { configurable: true, get: () => 800 })
Object.defineProperty(HTMLElement.prototype, 'clientHeight', { configurable: true, get: () => 280 })
const freezeClock = () => {
  vi.useFakeTimers({ toFake: ['Date'] })
  vi.setSystemTime(new Date('2026-10-08T12:00:00Z'))
}
freezeClock()
const { server, resetHttp } = await import('./http-support')
server.listen({ onUnhandledRequest: 'error' })
beforeEach(freezeClock)
afterEach(() => { cleanup(); server.resetHandlers(); resetHttp(); vi.restoreAllMocks(); vi.useRealTimers(); window.history.replaceState({}, '', '/') })
afterAll(() => server.close())
