// Kept apart from useFormat: the formatter is large, and this way it only
// ships with the lazy-loaded database page that shows query details.
import { formatDialect, postgresql } from 'sql-formatter'

/**
 * SQL laid out one clause per line for reading. Only whitespace changes;
 * SQL the formatter can't parse (often a rejected query) comes back as given.
 */
export function formatSql(sql) {
  if (!sql) return ''
  try {
    return formatDialect(String(sql), { dialect: postgresql, tabWidth: 2 })
  } catch {
    return String(sql)
  }
}
