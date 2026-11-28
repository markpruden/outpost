# Drag and drop upload: files dropped on the project's Threads panel land in its Artifacts;
# the panel shows a drop hint while files are dragged over it.
# Shots in $OUTPOST_E2E_DIR/shots/projdrop/.
import os
from playwright.async_api import async_playwright
import asyncio
E = os.environ.get("OUTPOST_E2E_DIR", ".") + "/"
OUT = E + "shots/projdrop/"
os.makedirs(OUT, exist_ok=True)
U = "https://127.0.0.1:18460"
POST = "(a) => fetch(a[0], {method: 'POST', headers: {'Content-Type': 'application/json'}, body: JSON.stringify(a[1] || {})}).then(r => r.json())"
GET = "(u) => fetch(u).then(r => r.json())"
DROP = """(sel) => {
  const dt = new DataTransfer();
  dt.items.add(new File(['hello'], 'dropped.txt', {type: 'text/plain'}));
  const t = document.querySelector(sel);
  t.dispatchEvent(new DragEvent('dragover', {dataTransfer: dt, bubbles: true, cancelable: true}));
  const hint = t.classList.contains('drop-on');
  t.dispatchEvent(new DragEvent('drop', {dataTransfer: dt, bubbles: true, cancelable: true}));
  return hint;
}"""

async def until(pg, expr, timeout=15000):
    for _ in range(timeout // 100):
        if await pg.evaluate(f"() => !!({expr})"):
            return
        await pg.wait_for_timeout(100)
    raise AssertionError("timed out: " + expr)

async def main():
    async with async_playwright() as p:
        b = await p.chromium.launch(**({"executable_path": os.environ["PW_CHROMIUM"]} if os.environ.get("PW_CHROMIUM") else {}))
        ctx = await b.new_context(ignore_https_errors=True, viewport={"width": 1400, "height": 900})
        pg = await ctx.new_page()
        await pg.goto(U + "/"); await pg.fill("#pw", open(E + "pw.txt").read().strip()); await pg.click("button[type=submit]")
        await pg.wait_for_selector("#nt-prompt")
        pid = (await pg.evaluate(POST, ["/api/projects", {"name": "Drop upload"}]))["id"]
        await pg.goto(U + "/#/projects/" + pid)
        await pg.click("#pj-threads-btn")
        await until(pg, "document.querySelector('#pj-panel .pj-sec.arts')")
        assert await pg.evaluate(DROP, "#pj-panel"), "no drop hint while dragging"
        await until(pg, "document.querySelector('#pj-panel .pj-acard')")
        l = await pg.evaluate(GET, "/api/projects/" + pid + "/artifacts")
        assert [f["path"] for f in l["files"]] == ["dropped.txt"], l
        assert not await pg.evaluate("() => document.querySelector('#pj-panel').classList.contains('drop-on')")
        await pg.screenshot(path=OUT + "after-drop.png")
        await b.close()
    print("projdrop OK")
asyncio.run(main())
