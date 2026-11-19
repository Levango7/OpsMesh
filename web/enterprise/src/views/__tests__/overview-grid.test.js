import { describe, it, expect } from 'vitest'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'

const src = readFileSync(resolve(__dirname, '../OverviewView.vue'), 'utf8')

describe('Overview KPI 中宽度栅格', () => {
  it('768–1100px 固定两列，四张 KPI 形成 2×2，小屏不被强行双列', () => {
    expect(src).toMatch(/@media\s*\(min-width:\s*768px\)\s*and\s*\(max-width:\s*1100px\)/)
    const mediaStart = src.indexOf('@media (min-width: 768px) and (max-width: 1100px)')
    const mediaEnd = src.indexOf('.stats-skeleton-card', mediaStart)
    const block = src.slice(mediaStart, mediaEnd)
    expect(block).toContain('.stats-grid,')
    expect(block).toContain('.stats-skeleton')
    expect(block).toMatch(/grid-template-columns:\s*repeat\(2,\s*minmax\(0,\s*1fr\)\)/)
  })
})
