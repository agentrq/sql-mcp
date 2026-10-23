import { afterEach, describe, expect, it, vi } from 'vitest'
import { useToasts } from '../src/composables/useToasts'
import { copyToClipboard } from '../src/composables/useClipboard'
import { formatCell, formatDateTime, formatDuration, toKebabCase } from '../src/composables/useFormat'
import { useQueryConsole } from '../src/composables/useQueryConsole'
import { ApiError } from '../src/api'

describe('useToasts', () => {
  afterEach(() => { useToasts().toasts.value = []; vi.useRealTimers() })

  it('adds, caps, expires and removes', () => {
    vi.useFakeTimers()
    const t = useToasts()
    t.notifySuccess('a')
    t.notifyInfo('b', 'B')
    const id = t.notifyError('c')
    expect(t.toasts.value.map((x) => x.type)).toEqual(['success', 'info', 'error'])
    expect(t.toasts.value[2].duration).toBe(10000)
    t.addToast('d', 'info', null, 0)
    t.addToast('e')
    expect(t.toasts.value).toHaveLength(4)
    expect(t.toasts.value.at(-2).persistent).toBe(true)
    t.removeToast(id)
    expect(t.toasts.value.some((x) => x.id === id)).toBe(false)
    vi.advanceTimersByTime(4001)
    expect(t.toasts.value.map((x) => x.message)).toEqual(['d'])
  })

  it('gives toasts added in the same millisecond distinct ids', () => {
    vi.useFakeTimers()
    const t = useToasts()
    const ids = Array.from({ length: 50 }, () => t.addToast('x', 'info', null, 0))
    expect(new Set(ids).size).toBe(50)
  })
})

describe('copyToClipboard', () => {
  afterEach(() => { useToasts().toasts.value = [] })
  it('copies and toasts, or reports failure', async () => {
    const writeText = vi.fn(async () => {})
    Object.defineProperty(navigator, 'clipboard', { value: { writeText }, configurable: true })
    expect(await copyToClipboard('x', 'Done')).toBe(true)
    expect(writeText).toHaveBeenCalledWith('x')
    expect(useToasts().toasts.value.at(-1).message).toBe('Done')
    writeText.mockRejectedValueOnce(new Error('no'))
    expect(await copyToClipboard('x')).toBe(false)
    expect(useToasts().toasts.value.at(-1).type).toBe('error')
  })
})

describe('useFormat', () => {
  it('kebab-cases', () => {
    expect(toKebabCase('  My Analytics DB! ')).toBe('my-analytics-db')
    expect(toKebabCase('')).toBe('')
    expect(toKebabCase(null)).toBe('')
  })
  it('formats dates, durations and cells', () => {
    expect(formatDateTime('')).toBe('')
    expect(formatDateTime('nope')).toBe('nope')
    expect(formatDateTime('2026-01-02T03:04:05Z')).toMatch(/2026/)
    expect(formatDuration(null)).toBe('')
    expect(formatDuration('x')).toBe('')
    expect(formatDuration(12.4)).toBe('12 ms')
    expect(formatDuration(1450)).toBe('1.4 s')
    expect(formatCell(null)).toBe('NULL')
    expect(formatCell(undefined)).toBe('NULL')
    expect(formatCell({ a: 1 })).toBe('{"a":1}')
    expect(formatCell(3)).toBe('3')
    expect(formatCell(false)).toBe('false')
  })
})

describe('useQueryConsole', () => {
  it('runs trimmed SQL and normalises the result', async () => {
    const run = vi.fn(async () => ({ columns: ['id'], rows: [[1], [2]], truncated: true, resultUrl: 'https://x' }))
    const c = useQueryConsole('db1', run)
    c.sql.value = '  select 1  '
    await c.execute()
    expect(run).toHaveBeenCalledWith('db1', 'select 1')
    expect(c.result.value).toEqual({ columns: ['id'], rows: [[1], [2]], rowCount: 2, truncated: true, resultUrl: 'https://x' })
    expect(c.running.value).toBe(false)
  })

  it('accepts a getter id and an empty answer', async () => {
    const run = vi.fn(async () => null)
    const c = useQueryConsole(() => 'db2', run)
    c.sql.value = 'select 1'
    await c.execute()
    expect(run).toHaveBeenCalledWith('db2', 'select 1')
    expect(c.result.value).toEqual({ columns: [], rows: [], rowCount: 0, truncated: false, resultUrl: '' })
  })

  it('does nothing for blank SQL or while running', async () => {
    const run = vi.fn(() => new Promise(() => {}))
    const c = useQueryConsole('db', run)
    await c.execute()
    expect(run).not.toHaveBeenCalled()
    c.sql.value = 'select 1'
    c.execute()
    c.execute()
    expect(run).toHaveBeenCalledTimes(1)
  })

  it('shows a 422 as a rejection and anything else as an error', async () => {
    const c = useQueryConsole('db', vi.fn(async () => { throw new ApiError('touches users.email', 422) }))
    c.sql.value = 'select email from users'
    await c.execute()
    expect(c.rejection.value).toBe('touches users.email')
    expect(c.error.value).toBe('')
    expect(c.result.value).toBeNull()

    const d = useQueryConsole('db', vi.fn(async () => { throw new ApiError('syntax error', 400) }))
    d.sql.value = 'selec'
    await d.execute()
    expect(d.error.value).toBe('syntax error')
    expect(d.rejection.value).toBe('')

    const e = useQueryConsole('db', vi.fn(async () => { throw {} }))
    e.sql.value = 'x'
    await e.execute()
    expect(e.error.value).toBe('Query failed')
  })

  it('runs on Cmd/Ctrl+Enter only', async () => {
    const run = vi.fn(async () => ({}))
    const c = useQueryConsole('db', run)
    c.sql.value = 'select 1'
    const ev = (o) => ({ key: 'Enter', preventDefault: vi.fn(), ...o })
    c.onKeydown(ev({}))
    c.onKeydown({ key: 'a', ctrlKey: true })
    expect(run).not.toHaveBeenCalled()
    const e1 = ev({ metaKey: true })
    c.onKeydown(e1)
    expect(e1.preventDefault).toHaveBeenCalled()
    await Promise.resolve()
    c.onKeydown(ev({ ctrlKey: true }))
    expect(run).toHaveBeenCalled()
  })
})
