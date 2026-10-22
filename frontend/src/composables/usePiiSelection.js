import { computed, ref, shallowRef } from 'vue'

const SEP = '\u0000'

/** The identity of one column; a separator no identifier can contain. */
export function columnKey(schema, table, column) {
  return [schema || '', table, column].join(SEP)
}

export function parseColumnKey(key) {
  const [schema, table, column] = key.split(SEP)
  return { schema, table, column }
}

export function tableKey(table) {
  return `${table.schema || ''}${SEP}${table.name}`
}

/** "public.users", or just "users" when the schema is empty. */
export function tableLabel(table) {
  return table.schema ? `${table.schema}.${table.name}` : table.name
}

const sameSet = (a, b) => a.size === b.size && [...a].every((k) => b.has(k))

/**
 * Selection state for the PII editor: which columns are marked, what is
 * unsaved, and the search over many tables. Pure logic, so the view stays a
 * thin template over it.
 *
 * Keys are `schema\0table\0column`. The initial selection is the schema's
 * `pii` flags, plus any `piiColumns` the server listed separately.
 */
export function usePiiSelection() {
  const tables = shallowRef([])
  const selected = ref(new Set())
  const saved = ref(new Set())
  const query = ref('')

  /** Load a schema (`GET /schema` tables) and optionally the saved PII list. */
  function load(schemaTables = [], piiColumns = null) {
    tables.value = Array.isArray(schemaTables) ? schemaTables : []
    const initial = new Set()
    for (const t of tables.value) {
      for (const c of t.columns || []) {
        if (c.pii) initial.add(columnKey(t.schema, t.name, c.name))
      }
    }
    for (const p of piiColumns || []) initial.add(columnKey(p.schema, p.table, p.column))
    selected.value = new Set(initial)
    saved.value = new Set(initial)
  }

  const isSelected = (table, column) => selected.value.has(columnKey(table.schema, table.name, column.name ?? column))

  function setColumn(table, column, on) {
    const key = columnKey(table.schema, table.name, column.name ?? column)
    const next = new Set(selected.value)
    if (on) next.add(key)
    else next.delete(key)
    selected.value = next
  }

  function toggle(table, column) {
    setColumn(table, column, !isSelected(table, column))
  }

  /** Mark (or clear) every column of one table. */
  function setTable(table, on) {
    const next = new Set(selected.value)
    for (const c of table.columns || []) {
      const key = columnKey(table.schema, table.name, c.name)
      if (on) next.add(key)
      else next.delete(key)
    }
    selected.value = next
  }

  /** Add every `suggestedPii` column (in the given tables, or all) to the selection. */
  function selectSuggested(within = tables.value) {
    const next = new Set(selected.value)
    for (const t of within) {
      for (const c of t.columns || []) {
        if (c.suggestedPii) next.add(columnKey(t.schema, t.name, c.name))
      }
    }
    selected.value = next
  }

  function clearAll() {
    selected.value = new Set()
  }

  /** Throw away unsaved changes. */
  function revert() {
    selected.value = new Set(saved.value)
  }

  const tableSelectedCount = (table) =>
    (table.columns || []).reduce((n, c) => n + (selected.value.has(columnKey(table.schema, table.name, c.name)) ? 1 : 0), 0)

  /** 'all' | 'some' | 'none' — what the table's header checkbox shows. */
  function tableState(table) {
    const total = (table.columns || []).length
    const n = tableSelectedCount(table)
    if (total > 0 && n === total) return 'all'
    return n > 0 ? 'some' : 'none'
  }

  const suggestedCount = computed(() =>
    tables.value.reduce((n, t) => n + (t.columns || []).filter((c) => c.suggestedPii).length, 0)
  )

  /** Suggested columns not yet selected. */
  const pendingSuggestedCount = computed(() =>
    tables.value.reduce(
      (n, t) => n + (t.columns || []).filter((c) => c.suggestedPii && !selected.value.has(columnKey(t.schema, t.name, c.name))).length,
      0
    )
  )

  const columnCount = computed(() => tables.value.reduce((n, t) => n + (t.columns || []).length, 0))

  /**
   * Tables matching the search. A table matches by its name (with or without
   * schema) and shows all its columns; otherwise it shows only the columns
   * whose name or type matches. Case-insensitive; blank shows everything.
   */
  const filteredTables = computed(() => {
    const q = query.value.trim().toLowerCase()
    if (!q) return tables.value
    const out = []
    for (const t of tables.value) {
      if (tableLabel(t).toLowerCase().includes(q)) {
        out.push(t)
        continue
      }
      const columns = (t.columns || []).filter(
        (c) => c.name.toLowerCase().includes(q) || (c.dataType || '').toLowerCase().includes(q)
      )
      if (columns.length) out.push({ ...t, columns, partial: true })
    }
    return out
  })

  const selectedCount = computed(() => selected.value.size)
  const dirty = computed(() => !sameSet(selected.value, saved.value))
  const addedCount = computed(() => [...selected.value].filter((k) => !saved.value.has(k)).length)
  const removedCount = computed(() => [...saved.value].filter((k) => !selected.value.has(k)).length)

  /** The `PUT /pii` body's list, sorted so saves are stable. */
  function toPayload() {
    return [...selected.value].sort().map(parseColumnKey)
  }

  /** After a save: the server's answer becomes both the saved and the current set. */
  function markSaved(piiColumns = null) {
    const next = piiColumns ? new Set(piiColumns.map((p) => columnKey(p.schema, p.table, p.column))) : new Set(selected.value)
    selected.value = new Set(next)
    saved.value = next
  }

  return {
    tables,
    selected,
    query,
    load,
    isSelected,
    setColumn,
    toggle,
    setTable,
    selectSuggested,
    clearAll,
    revert,
    tableSelectedCount,
    tableState,
    suggestedCount,
    pendingSuggestedCount,
    columnCount,
    filteredTables,
    selectedCount,
    dirty,
    addedCount,
    removedCount,
    toPayload,
    markSaved,
  }
}
