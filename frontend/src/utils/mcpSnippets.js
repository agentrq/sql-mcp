import { toKebabCase } from '../composables/useFormat'

/** The server name a client registers: the database's name, kebab-cased. */
export function mcpServerName(database) {
  return toKebabCase(database?.name) || 'sql-mcp'
}

export function claudeCodeCommand(database) {
  return `claude mcp add --transport http ${mcpServerName(database)} ${database?.mcpUrl || ''}`
}

export function mcpJsonConfig(database) {
  const config = { mcpServers: { [mcpServerName(database)]: { type: 'http', url: database?.mcpUrl || '' } } }
  return JSON.stringify(config, null, 2)
}
