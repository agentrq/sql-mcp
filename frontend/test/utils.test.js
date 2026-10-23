import { describe, expect, it } from 'vitest'
import { loginRedirect } from '../src/utils/loginRedirect'
import { claudeCodeCommand, mcpJsonConfig, mcpServerName } from '../src/utils/mcpSnippets'
import { driverName, DSN_FORMATS, DSN_PLACEHOLDERS, FALLBACK_DRIVERS } from '../src/utils/drivers'

const origin = 'https://sql.example.com'

describe('loginRedirect', () => {
  it('keeps same-origin paths and URLs', () => {
    expect(loginRedirect('/databases/1', origin)).toBe('https://sql.example.com/databases/1')
    expect(loginRedirect('https://sql.example.com/mcp/x/oauth2/authorize?a=1', origin)).toBe('https://sql.example.com/mcp/x/oauth2/authorize?a=1')
  })
  it('drops everything else', () => {
    expect(loginRedirect(undefined, origin)).toBeNull()
    expect(loginRedirect(['/a'], origin)).toBeNull()
    expect(loginRedirect('  ', origin)).toBeNull()
    expect(loginRedirect('https://evil.com/x', origin)).toBeNull()
    expect(loginRedirect('//evil.com/x', origin)).toBeNull()
    expect(loginRedirect('javascript:alert(1)', origin)).toBeNull()
    expect(loginRedirect('http://[bad', origin)).toBeNull()
  })
})

describe('mcpSnippets', () => {
  const db = { name: 'My Analytics', mcpUrl: 'https://sql.example.com/mcp/abc' }
  it('names the server and builds snippets', () => {
    expect(mcpServerName(db)).toBe('my-analytics')
    expect(mcpServerName({})).toBe('sql-mcp')
    expect(claudeCodeCommand(db)).toBe('claude mcp add --transport http my-analytics https://sql.example.com/mcp/abc')
    expect(JSON.parse(mcpJsonConfig(db))).toEqual({ mcpServers: { 'my-analytics': { type: 'http', url: 'https://sql.example.com/mcp/abc' } } })
    expect(claudeCodeCommand(null)).toBe('claude mcp add --transport http sql-mcp ')
    expect(JSON.parse(mcpJsonConfig(null)).mcpServers['sql-mcp'].url).toBe('')
  })
})

describe('drivers', () => {
  it('names drivers', () => {
    expect(driverName('postgres')).toBe('PostgreSQL')
    expect(driverName('oracle')).toBe('oracle')
    expect(driverName(undefined)).toBe('')
    expect(FALLBACK_DRIVERS.find((d) => d.id === 'mysql').available).toBe(true)
    expect(DSN_PLACEHOLDERS.postgres).toContain('sslmode=require')
    expect(DSN_PLACEHOLDERS.mysql).toMatch(/^mysql:\/\//)
    expect(DSN_FORMATS.mysql).toContain('tcp(host:3306)')
  })
})
