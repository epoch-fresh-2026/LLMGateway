import test from 'node:test'
import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'

// Reproduces a bug where an empty ranged model aggregate fell back to the
// unfiltered recent logs, so a date range with no usage showed tokens from
// outside that range. The model distribution must only use the ranged
// aggregate (empty range => empty distribution).
test('dashboard model distribution never falls back to unfiltered logs', async () => {
  const source = await readFile(new URL('../src/pages/DashboardPage.tsx', import.meta.url), 'utf8')
  assert.doesNotMatch(source, /dist\.entries|const dist\s*=/, 'model distribution must not aggregate unfiltered logRows')
})

test('consumption pages show settled totals and separated sources without request fallback', async () => {
  const dashboard = await readFile(new URL('../src/pages/DashboardPage.tsx', import.meta.url), 'utf8')
  const usage = await readFile(new URL('../src/pages/UsagePage.tsx', import.meta.url), 'utf8')
  for (const source of [dashboard, usage]) {
    assert.match(source, /actual_tokens/)
    assert.match(source, /estimated_tokens/)
    assert.match(source, /上游确认/)
    assert.match(source, /本地估算/)
    assert.match(source, /仅作零费用审计/)
    assert.doesNotMatch(source, /已结算消费\s*=|估算计入配额/)
    assert.doesNotMatch(source, /total_tokens\s*\|\|\s*row\.request_count/)
  }
})
