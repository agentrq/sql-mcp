// Kept apart from useFormat for the same reason as useSqlFormat: Prism only
// ships with the lazy-loaded database page that shows query details.
import Prism from 'prismjs'
import 'prismjs/components/prism-sql'

// Prism's SQL grammar is generic. Postgres differs where it would miscolour:
// `#` and `//` are operators (`data #> '{a}'`), not comments; a double-quoted
// name is an identifier, not a string; strings can be dollar-quoted;
// parameters are `$1`, not `@name`; and `::` casts and the `#`, `@` and `?`
// JSON operators are operators.
const grammar = Prism.languages.extend('sql', {
  comment: { pattern: /(^|[^\\])(?:\/\*[\s\S]*?\*\/|--.*)/, lookbehind: true },
  variable: /\$\d+\b/,
  string: { pattern: /\$([A-Za-z_]\w*)?\$[\s\S]*?\$\1\$|'(?:[^']|'')*'/, greedy: true },
  identifier: { pattern: /"(?:[^"]|"")*"/, greedy: true },
  operator: /::|[-+*/=%^~#@?]|&&?|\|\|?|!=?|<(?:=>?|<|>)?|>[>=]?/,
  // Coloured as keywords: grey like `=`, `and` and `in` fade into the punctuation.
  'word-operator': { pattern: /\b(?:AND|BETWEEN|ILIKE|IN|IS|LIKE|NOT|OR|SIMILAR TO)\b/i, alias: 'keyword' },
})

// MySQL reads it differently again: `#` starts a comment, a backquoted name
// is an identifier, both quotes make strings and `\` escapes inside them,
// and `@name` and `@@name` are variables.
const mysqlGrammar = Prism.languages.extend('sql', {
  comment: { pattern: /(^|[^\\])(?:\/\*[\s\S]*?\*\/|(?:--(?=\s)|#).*)/, lookbehind: true },
  variable: /@@?[\w.$]+/,
  string: { pattern: /'(?:\\[\s\S]|[^'\\]|'')*'|"(?:\\[\s\S]|[^"\\]|"")*"/, greedy: true },
  identifier: { pattern: /`(?:[^`]|``)*`/, greedy: true },
  operator: /:=|[-+*/=%^~]|&&?|\|\|?|!=?|<(?:=>?|<|>)?|>[>=]?/,
  'word-operator': { pattern: /\b(?:AND|BETWEEN|DIV|IN|IS|LIKE|MOD|NOT|OR|REGEXP|RLIKE|XOR)\b/i, alias: 'keyword' },
})

/**
 * The SQL as HTML, each token wrapped in a `<span class="token …">`, read the
 * way the driver's database reads it (Postgres unless it is `mysql`). Prism
 * escapes the text, so the result is safe for v-html.
 */
export function highlightSql(sql, driver) {
  const text = sql ? String(sql) : ''
  return driver === 'mysql' ? Prism.highlight(text, mysqlGrammar, 'mysql') : Prism.highlight(text, grammar, 'pgsql')
}
