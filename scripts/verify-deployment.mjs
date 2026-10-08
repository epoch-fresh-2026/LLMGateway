import assert from 'node:assert/strict'
import { appendFileSync } from 'node:fs'
import { setTimeout } from 'node:timers/promises'

const base = new URL(process.argv[2] ?? process.env.DEPLOYMENT_URL ?? '')
assert.equal(base.protocol, 'https:', 'Deployment URL must use HTTPS')
const results = []

async function check(path, status, validate) {
  for (let attempt = 1; ; attempt++) {
    try {
      const response = await fetch(new URL(path, base), { signal: AbortSignal.timeout(20_000) })
      assert.equal(response.status, status, `${path}: unexpected HTTP status`)
      const body = await response.text()
      validate(response, body)
      results.push({ path, status })
      console.log(`PASS ${path} -> ${status}`)
      return body
    } catch (error) {
      if (attempt === 3) throw error
      console.log(`Retry ${path} (${attempt}/3)`)
      await setTimeout(attempt * 1000)
    }
  }
}

function json(response, body) {
  assert.match(response.headers.get('content-type') ?? '', /application\/json/)
  return JSON.parse(body)
}

function dashboard(response, body) {
  assert.match(response.headers.get('content-type') ?? '', /text\/html/)
  assert.match(body, /LLMGateway Dashboard/)
}

try {
  await check('/healthz', 200, (response, body) => assert.equal(json(response, body).status, 'ok'))
  const html = await check('/login', 200, dashboard)
  await check('/channels', 200, dashboard)
  await check('/admin/auth/me', 401, (response, body) => assert.equal(json(response, body).code, 401))
  await check('/v1/models', 401, (response, body) => assert.ok(json(response, body).error))
  const assets = [...new Set([...html.matchAll(/\b(?:src|href)="(\/assets\/[^"?#]+)"/g)].map(match => match[1]))]
  assert.ok(assets.some(path => path.endsWith('.js')), 'Missing dashboard JavaScript asset')
  assert.ok(assets.some(path => path.endsWith('.css')), 'Missing dashboard CSS asset')
  for (const path of assets) {
    await check(path, 200, (response, body) => {
      assert.match(response.headers.get('content-type') ?? '', path.endsWith('.css') ? /text\/css/ : /javascript|ecmascript/)
      assert.ok(body.length > 0, `${path}: empty asset`)
    })
  }
  console.log(`Deployment verified: ${base.origin}`)
} catch (error) {
  console.error(`Deployment verification failed: ${error.message}`)
  process.exitCode = 1
} finally {
  if (process.env.GITHUB_STEP_SUMMARY) {
    const rows = results.map(({ path, status }) => `| ${path} | ${status} |`).join('\n')
    appendFileSync(process.env.GITHUB_STEP_SUMMARY, `Production check: ${process.exitCode ? 'FAILED' : 'PASSED'}\n\n${base.origin}\n\n| Path | HTTP status |\n| --- | --- |\n${rows}\n`)
  }
}
