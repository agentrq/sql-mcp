import { describe, expect, it } from 'vitest'
import { columnKey, parseColumnKey, tableKey, tableLabel, usePiiSelection } from '../src/composables/usePiiSelection'

const users = {
  schema: 'public',
  name: 'users',
  columns: [
    { name: 'id', dataType: 'bigint', pii: false, suggestedPii: false },
    { name: 'email', dataType: 'text', pii: true, suggestedPii: true },
    { name: 'phone', dataType: 'text', pii: false, suggestedPii: true },
    { name: 'created_at', dataType: 'timestamptz' },
  ],
}
const orders = {
  schema: 'sales',
  name: 'orders',
  columns: [
    { name: 'id', dataType: 'bigint' },
    { name: 'shipping_address', dataType: 'jsonb', suggestedPii: true },
  ],
}
const empty = { schema: 'public', name: 'empty_table', columns: [] }

function setup(tables = [users, orders, empty], saved = null) {
  const s = usePiiSelection()
  s.load(tables, saved)
  return s
}

describe('column keys', () => {
  it('round-trips and labels', () => {
    const k = columnKey('public', 'users', 'email')
    expect(parseColumnKey(k)).toEqual({ schema: 'public', table: 'users', column: 'email' })
    expect(columnKey(undefined, 't', 'c')).toBe(columnKey('', 't', 'c'))
    expect(tableKey(users)).not.toBe(tableKey(orders))
    expect(tableKey({ name: 'x' })).toBe(tableKey({ schema: '', name: 'x' }))
    expect(tableLabel(users)).toBe('public.users')
    expect(tableLabel({ name: 'x' })).toBe('x')
  })

  it('does not confuse dotted names', () => {
    expect(columnKey('a.b', 'c', 'd')).not.toBe(columnKey('a', 'b.c', 'd'))
  })
})

describe('usePiiSelection', () => {
  it('starts from the schema pii flags, clean', () => {
    const s = setup()
    expect(s.selectedCount.value).toBe(1)
    expect(s.isSelected(users, users.columns[1])).toBe(true)
    expect(s.isSelected(users, 'email')).toBe(true)
    expect(s.dirty.value).toBe(false)
    expect(s.columnCount.value).toBe(6)
    expect(s.suggestedCount.value).toBe(3)
    expect(s.pendingSuggestedCount.value).toBe(2)
  })

  it('unions a separately saved PII list', () => {
    const s = setup([users, orders], [{ schema: 'sales', table: 'orders', column: 'id' }])
    expect(s.selectedCount.value).toBe(2)
    expect(s.isSelected(orders, 'id')).toBe(true)
    expect(s.dirty.value).toBe(false)
  })

  it('tolerates junk input', () => {
    const s = usePiiSelection()
    s.load(null)
    expect(s.tables.value).toEqual([])
    s.load()
    expect(s.selectedCount.value).toBe(0)
    s.load([{ schema: 'x', name: 'nocols' }])
    expect(s.columnCount.value).toBe(0)
    expect(s.tableState({ name: 'nocols' })).toBe('none')
    s.setTable({ name: 'nocols' }, true)
    s.selectSuggested()
    expect(s.selectedCount.value).toBe(0)
  })

  it('toggles single columns and tracks dirty/added/removed', () => {
    const s = setup()
    s.toggle(users, users.columns[2])
    expect(s.isSelected(users, 'phone')).toBe(true)
    expect(s.dirty.value).toBe(true)
    expect(s.addedCount.value).toBe(1)
    s.setColumn(users, 'email', false)
    expect(s.removedCount.value).toBe(1)
    expect(s.selectedCount.value).toBe(1)
    s.toggle(users, 'phone')
    s.toggle(users, 'email')
    expect(s.dirty.value).toBe(false)
  })

  it('marks and clears a whole table', () => {
    const s = setup()
    expect(s.tableState(users)).toBe('some')
    s.setTable(users, true)
    expect(s.tableState(users)).toBe('all')
    expect(s.tableSelectedCount(users)).toBe(4)
    expect(s.tableState(orders)).toBe('none')
    s.setTable(users, false)
    expect(s.tableState(users)).toBe('none')
    expect(s.selectedCount.value).toBe(0)
  })

  it('selects suggested columns, everywhere or within some tables', () => {
    const s = setup()
    s.selectSuggested([orders])
    expect(s.isSelected(orders, 'shipping_address')).toBe(true)
    expect(s.isSelected(users, 'phone')).toBe(false)
    s.selectSuggested()
    expect(s.isSelected(users, 'phone')).toBe(true)
    expect(s.pendingSuggestedCount.value).toBe(0)
    expect(s.selectedCount.value).toBe(3)
  })

  it('clears all and reverts', () => {
    const s = setup()
    s.clearAll()
    expect(s.selectedCount.value).toBe(0)
    expect(s.dirty.value).toBe(true)
    s.revert()
    expect(s.selectedCount.value).toBe(1)
    expect(s.dirty.value).toBe(false)
  })

  it('searches tables by name and columns by name or type', () => {
    const s = setup()
    expect(s.filteredTables.value).toHaveLength(3)
    s.query.value = '  USERS '
    expect(s.filteredTables.value).toEqual([users])
    s.query.value = 'sales.'
    expect(s.filteredTables.value).toEqual([orders])
    s.query.value = 'phone'
    expect(s.filteredTables.value).toHaveLength(1)
    expect(s.filteredTables.value[0].columns.map((c) => c.name)).toEqual(['phone'])
    expect(s.filteredTables.value[0].partial).toBe(true)
    s.query.value = 'jsonb'
    expect(s.filteredTables.value[0].name).toBe('orders')
    s.query.value = 'nothing-matches'
    expect(s.filteredTables.value).toEqual([])
  })

  it('selection through a filtered table hits the real columns', () => {
    const s = setup()
    s.query.value = 'phone'
    const filtered = s.filteredTables.value[0]
    s.setTable(filtered, true)
    s.query.value = ''
    expect(s.isSelected(users, 'phone')).toBe(true)
    expect(s.isSelected(users, 'id')).toBe(false)
  })

  it('builds a sorted payload and adopts the saved answer', () => {
    const s = setup()
    s.setColumn(orders, 'shipping_address', true)
    s.setColumn(users, 'phone', true)
    const payload = s.toPayload()
    expect(payload).toEqual([
      { schema: 'public', table: 'users', column: 'email' },
      { schema: 'public', table: 'users', column: 'phone' },
      { schema: 'sales', table: 'orders', column: 'shipping_address' },
    ])
    s.markSaved(payload.slice(0, 2))
    expect(s.dirty.value).toBe(false)
    expect(s.selectedCount.value).toBe(2)
    s.toggle(users, 'id')
    s.markSaved()
    expect(s.dirty.value).toBe(false)
    expect(s.selectedCount.value).toBe(3)
  })

  it('applies its changes onto a set that changed on the server', () => {
    const s = setup()
    s.setColumn(users, 'email', false)
    s.setColumn(users, 'phone', true)
    const latest = [
      { schema: 'public', table: 'users', column: 'email' },
      { schema: 'sales', table: 'orders', column: 'shipping_address' }, // the agent marked it meanwhile
    ]
    expect(s.toPayloadOnto(latest)).toEqual([
      { schema: 'public', table: 'users', column: 'phone' },
      { schema: 'sales', table: 'orders', column: 'shipping_address' },
    ])
    expect(s.toPayloadOnto()).toEqual([{ schema: 'public', table: 'users', column: 'phone' }])
  })
})
