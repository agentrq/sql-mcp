const NAMES = { postgres: 'PostgreSQL', mysql: 'MySQL' }

/** A driver id as people say it. */
export function driverName(id) {
  return NAMES[id] || id || ''
}

/** Used when GET /drivers fails, so the form still renders. */
export const FALLBACK_DRIVERS = [
  { id: 'postgres', name: 'PostgreSQL', available: true },
  { id: 'mysql', name: 'MySQL', available: true },
]

export const DSN_PLACEHOLDERS = {
  postgres: 'postgres://user:pass@host:5432/db?sslmode=require',
  mysql: 'mysql://user:pass@host:3306/db?tls=true',
}

/** The DSN forms each driver reads. */
export const DSN_FORMATS = {
  postgres: 'A postgres:// URL, or key=value pairs such as host=h user=u dbname=db.',
  mysql: 'A mysql:// URL, or the Go driver form user:pass@tcp(host:3306)/db?tls=true. MariaDB works too.',
}
