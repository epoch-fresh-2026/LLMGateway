import { readFileSync, readdirSync } from 'node:fs'
import { resolve, relative } from 'node:path'
import ts from 'typescript'

const coverage = JSON.parse(readFileSync('coverage/coverage-final.json', 'utf8'))
const normalized = path => resolve(path).replaceAll('\\', '/')
const reported = new Map(Object.entries(coverage).map(([path, data]) => [normalized(path), data]))
const expected = readdirSync('src', { recursive: true }).filter(path => /\.tsx?$/.test(path) && !/\.d\.ts$/.test(path) && normalized(resolve('src', path)) !== normalized('src/api/generated/schema.ts')).map(path => resolve('src', path)).filter(path => {
  const output = ts.transpileModule(readFileSync(path, 'utf8'), { compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.ESNext, jsx: ts.JsxEmit.ReactJSX, verbatimModuleSyntax: true } }).outputText
  return output.replace(/export\s*\{\s*\};?\s*/g, '').trim() !== ''
})
const missing = expected.filter(path => !reported.has(normalized(path)))
if (missing.length) throw new Error(`Runtime source missing from coverage: ${missing.map(path => relative('.', path)).join(', ')}`)
let total = 0, covered = 0
const uncovered = []
for (const [path, data] of reported) {
  for (const [id, hits] of Object.entries(data.s)) {
    total++
    if (hits > 0) covered++
    else uncovered.push(`${relative('.', path)}:${data.statementMap[id].start.line}`)
  }
}
if (total === 0) throw new Error('Coverage contains zero production statements')
if (uncovered.length) throw new Error(`${uncovered.length} uncovered production statements: ${uncovered.join(', ')}`)
console.log(`Exact production coverage: ${covered}/${total} statements, ${expected.length} runtime source files, zero uncovered statements.`)
