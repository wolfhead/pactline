// scratch-shot.mjs —— 后续任务反复用它，先建好
import { chromium } from '@playwright/test'
const [path = '/tasks', tag = 'shell'] = process.argv.slice(2)
const b = await chromium.launch()
for (const scheme of ['light', 'dark']) {
  for (const [w, h, tier] of [[1440, 900, 'xl'], [1120, 820, 'lg'], [900, 800, 'md'], [390, 844, 'phone']]) {
    const ctx = await b.newContext({ viewport: { width: w, height: h } })
    const p = await ctx.newPage()
    await p.goto('http://localhost:5173/')
    await p.evaluate((s) => localStorage.setItem('bountyboard.theme.v2', s), scheme)
    await p.goto(`http://localhost:5173${path}`)
    await p.waitForTimeout(900)
    await p.screenshot({ path: `/tmp/${tag}-${tier}-${scheme}.png`, fullPage: false })
    await ctx.close()
  }
}
await b.close()
