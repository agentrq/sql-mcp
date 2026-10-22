package oauthconsent

// pageHTML follows the web app's sign-in card (LoginView.vue): the same
// colours, radii and logo, and the same theme choice applied before paint.
const pageHTML = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="robots" content="noindex">
<title>Allow access to SQL MCP?</title>
<script nonce="{{.Nonce}}">
(function () {
  var theme = null
  try { theme = localStorage.getItem('theme') } catch (e) {}
  var dark = theme === 'dark' || (theme !== 'light' && window.matchMedia && window.matchMedia('(prefers-color-scheme: dark)').matches)
  if (dark) document.documentElement.classList.add('dark')
})()
</script>
<style>
:root {
  --page: #f9fafb; --card: #ffffff; --card-border: #f3f4f6; --row: #f9fafb; --row-border: #f3f4f6;
  --text: #111827; --muted: #6b7280; --label: #9ca3af; --logo: #000000; --logo-ink: #ffffff;
  --primary: #111827; --primary-hover: #000000; --primary-text: #ffffff;
  --secondary: #ffffff; --secondary-hover: #f9fafb; --secondary-text: #374151; --secondary-border: #e5e7eb;
  --warn-bg: #fffbeb; --warn-border: #fde68a; --warn-text: #92400e;
}
.dark {
  --page: #09090b; --card: #18181b; --card-border: #27272a; --row: rgba(39, 39, 42, 0.5); --row-border: rgba(63, 63, 70, 0.5);
  --text: #fafafa; --muted: #a1a1aa; --label: #71717a; --logo: #ffffff; --logo-ink: #000000;
  --primary: #fafafa; --primary-hover: #e4e4e7; --primary-text: #09090b;
  --secondary: #27272a; --secondary-hover: #3f3f46; --secondary-text: #e4e4e7; --secondary-border: #3f3f46;
  --warn-bg: rgba(120, 53, 15, 0.2); --warn-border: rgba(120, 53, 15, 0.4); --warn-text: #fcd34d;
}
* { box-sizing: border-box; }
html, body { margin: 0; }
body {
  min-height: 100vh; display: flex; align-items: center; justify-content: center; padding: 16px;
  background: var(--page); color: var(--text);
  font-family: "Inter Variable", "Inter", ui-sans-serif, system-ui, -apple-system, "Segoe UI", sans-serif;
  -webkit-font-smoothing: antialiased;
}
.card { width: 100%; max-width: 28rem; padding: 32px; background: var(--card); border: 1px solid var(--card-border); border-radius: 24px; box-shadow: 0 25px 50px -12px rgba(0, 0, 0, 0.25); }
.logo { width: 64px; height: 64px; margin: 0 auto 24px; border-radius: 16px; background: var(--logo); color: var(--logo-ink); display: flex; align-items: center; justify-content: center; transform: rotate(3deg); }
.logo svg { width: 36px; height: 36px; }
h1 { margin: 0 0 8px; font-size: 24px; line-height: 1.25; font-weight: 900; letter-spacing: -0.025em; text-align: center; overflow-wrap: anywhere; }
.lead { margin: 0 0 20px; font-size: 14px; line-height: 1.6; color: var(--muted); text-align: center; }
.badge { display: block; margin: 0 0 20px; padding: 12px 16px; border-radius: 16px; border: 1px solid var(--warn-border); background: var(--warn-bg); color: var(--warn-text); font-size: 13px; line-height: 1.5; font-weight: 500; }
.rows { margin: 0 0 20px; border: 1px solid var(--row-border); border-radius: 16px; background: var(--row); }
.row { padding: 12px 16px; }
.row + .row { border-top: 1px solid var(--row-border); }
.row dt { margin: 0 0 2px; font-size: 11px; font-weight: 700; letter-spacing: 0.05em; text-transform: uppercase; color: var(--label); }
.row dd { margin: 0; font-size: 14px; line-height: 1.5; overflow-wrap: anywhere; }
.hint { margin: 0 0 24px; font-size: 13px; line-height: 1.6; color: var(--muted); text-align: center; }
.actions { display: flex; gap: 12px; }
button { flex: 1; padding: 16px 24px; border-radius: 16px; font: inherit; font-size: 15px; font-weight: 700; cursor: pointer; }
button:active { transform: scale(0.98); }
.deny { background: var(--secondary); color: var(--secondary-text); border: 1px solid var(--secondary-border); }
.deny:hover { background: var(--secondary-hover); }
.allow { background: var(--primary); color: var(--primary-text); border: 1px solid var(--primary); }
.allow:hover { background: var(--primary-hover); }
</style>
</head>
<body>
<main class="card">
  <div class="logo" aria-hidden="true">
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
      <ellipse cx="12" cy="5" rx="8" ry="3" /><path d="M4 5v14c0 1.66 3.58 3 8 3s8-1.34 8-3V5" /><path d="M4 12c0 1.66 3.58 3 8 3s8-1.34 8-3" />
    </svg>
  </div>
  <h1 data-test="consent-client">{{if .Named}}{{.ClientName}}{{else}}An unnamed app{{end}}</h1>
  <p class="lead">wants to query a database through SQL MCP.</p>
  {{if .Registered}}
  <p class="badge" data-test="consent-identity">Unverified name. Any app can call itself anything, so check where it sends you back.</p>
  {{else}}
  <p class="badge" data-test="consent-identity">Unknown app. SQL MCP has no record of who made it, so check where it sends you back.</p>
  {{end}}
  <dl class="rows">
    <div class="row"><dt>It will be able to</dt><dd data-test="consent-access">Run read-only queries on “{{.DatabaseName}}”, except on columns you marked as PII, and store results on your tmphost account when it is connected.</dd></div>
    <div class="row"><dt>Sends you back to</dt><dd data-test="consent-destination">{{.Destination}}</dd></div>
    {{if .Email}}<div class="row"><dt>Signed in as</dt><dd data-test="consent-account">{{.Email}}</dd></div>{{end}}
  </dl>
  <p class="hint">Only allow an app you just asked to connect. If you didn’t, deny.</p>
  <form method="post" class="actions">
    <input type="hidden" name="consent" value="{{.Token}}">
    <button type="submit" name="decision" value="deny" class="deny" data-test="consent-deny">Deny</button>
    <button type="submit" name="decision" value="allow" class="allow" data-test="consent-allow">Allow</button>
  </form>
</main>
</body>
</html>
`
