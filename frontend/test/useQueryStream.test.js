import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createQueryStream } from '../src/composables/useQueryStream'

/** A stand-in EventSource the test drives by hand. */
class FakeEventSource {
  static CONNECTING = 0
  static OPEN = 1
  static CLOSED = 2
  static instances = []

  constructor(url) {
    this.url = url
    this.readyState = FakeEventSource.CONNECTING
    this.listeners = {}
    this.closed = false
    FakeEventSource.instances.push(this)
  }

  addEventListener(type, fn) {
    this.listeners[type] = fn
  }

  close() {
    this.closed = true
    this.readyState = FakeEventSource.CLOSED
  }

  open() {
    this.readyState = FakeEventSource.OPEN
    this.onopen()
  }

  emit(type, data) {
    this.listeners[type]({ data })
  }

  /** The server refused it: EventSource gives up. */
  refuse() {
    this.readyState = FakeEventSource.CLOSED
    this.onerror()
  }

  /** The stream dropped: the browser retries by itself. */
  drop() {
    this.readyState = FakeEventSource.CONNECTING
    this.onerror()
  }
}

const latest = () => FakeEventSource.instances.at(-1)

function setup(overrides = {}) {
  const onQuery = vi.fn()
  const onOpen = vi.fn()
  const refresh = vi.fn(async () => {})
  const stream = createQueryStream({
    url: () => '/api/v1/databases/d1/queries/stream',
    onQuery,
    onOpen,
    refresh,
    EventSourceImpl: FakeEventSource,
    maxFailures: 2,
    backoffMs: 100,
    ...overrides,
  })
  return { stream, onQuery, onOpen, refresh }
}

describe('createQueryStream', () => {
  beforeEach(() => {
    FakeEventSource.instances = []
    vi.useFakeTimers()
  })
  afterEach(() => vi.useRealTimers())

  it('delivers queries once the stream is open', () => {
    const { stream, onQuery, onOpen } = setup()
    stream.start()
    expect(latest().url).toBe('/api/v1/databases/d1/queries/stream')
    expect(stream.status.value).toBe('connecting')
    latest().open()
    expect(stream.status.value).toBe('live')
    expect(onOpen).toHaveBeenCalledTimes(1)
    latest().emit('query', '{"id":"q1"}')
    latest().emit('query', 'not json')
    expect(onQuery).toHaveBeenCalledTimes(1)
    expect(onQuery).toHaveBeenCalledWith({ id: 'q1' })
  })

  it('lets the browser retry a dropped stream', () => {
    const { stream } = setup()
    stream.start()
    latest().open()
    latest().drop()
    expect(stream.status.value).toBe('connecting')
    expect(FakeEventSource.instances).toHaveLength(1)
  })

  it('renews the session and reconnects after a refusal, then gives up', async () => {
    const { stream, refresh, onOpen } = setup()
    stream.start()
    latest().refuse()
    expect(latest().closed).toBe(true)
    await vi.advanceTimersByTimeAsync(100)
    expect(refresh).toHaveBeenCalledTimes(1)
    expect(FakeEventSource.instances).toHaveLength(2)

    latest().refuse()
    await vi.advanceTimersByTimeAsync(200)
    expect(FakeEventSource.instances).toHaveLength(3)
    latest().refuse()
    await vi.advanceTimersByTimeAsync(1000)
    expect(stream.status.value).toBe('offline')
    expect(FakeEventSource.instances).toHaveLength(3)

    stream.start()
    latest().open()
    expect(stream.status.value).toBe('live')
    expect(onOpen).toHaveBeenCalledTimes(1)
  })

  it('reconnects even when the session cannot be renewed', async () => {
    const { stream } = setup({ refresh: vi.fn(async () => { throw new Error('401') }) })
    stream.start()
    latest().refuse()
    await vi.advanceTimersByTimeAsync(100)
    expect(FakeEventSource.instances).toHaveLength(2)
  })

  it('stays down once stopped', async () => {
    const { stream } = setup()
    stream.start()
    const first = latest()
    first.refuse()
    stream.stop()
    await vi.advanceTimersByTimeAsync(1000)
    expect(FakeEventSource.instances).toHaveLength(1)
    first.onerror() // a late error from a stream already replaced is ignored
  })

  it('is offline without EventSource, and works with the defaults', () => {
    const { stream } = setup({ EventSourceImpl: undefined })
    stream.start()
    expect(stream.status.value).toBe('offline')

    vi.stubGlobal('EventSource', undefined)
    const bare = createQueryStream({ url: () => '/x', onQuery: () => {} })
    bare.start()
    expect(bare.status.value).toBe('offline')
    vi.unstubAllGlobals()

    const defaults = createQueryStream({ url: () => '/x', onQuery: () => {}, EventSourceImpl: FakeEventSource })
    defaults.start()
    latest().open()
    latest().refuse()
    expect(defaults.status.value).toBe('connecting')
    defaults.stop()
  })
})
