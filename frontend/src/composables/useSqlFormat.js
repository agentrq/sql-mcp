// Kept apart from useFormat: the formatter is large, and this way it only
// ships with the lazy-loaded database page that shows query details.
import { formatDialect, mysql, postgresql } from 'sql-formatter'

/**
 * SQL laid out one clause per line for reading, in the dialect of the
 * database's driver (Postgres unless it is `mysql`). Only whitespace changes;
 * SQL the formatter can't parse (often a rejected query) comes back as given.
 */
export function formatSql(sql, driver) {
  if (!sql) return ''
  try {
    return formatDialect(String(sql), { dialect: driver === 'mysql' ? mysql : postgresql, tabWidth: 2 })
  } catch {
    return String(sql)
  }
}
