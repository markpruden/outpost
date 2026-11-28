# Chats live in the left sidebar (no right-hand panel on the Chat page); a chat moves into a project and back from its
# menu; a project has several conversations that switch in its Threads pane; the Ask | Task | Research switch starts a
# task from a conversation and shows its card under the message. Light, dark and phone.
import asyncio, json, os, ssl, urllib.request, http.cookiejar
from playwright.async_api import async_playwright
E = os.environ.get("OUTPOST_E2E_DIR", ".") + "/"
S = E + "shots/chatsidebar/"
os.makedirs(S, exist_ok=True)
U = "https://127.0.0.1:18460"
pw = open(E + "pw.txt").read().strip()
ctxs = ssl.create_default_context(); ctxs.check_hostname = False; ctxs.verify_mode = ssl.CERT_NONE
jar = http.cookiejar.CookieJar()
op = urllib.request.build_opener(urllib.request.HTTPSHandler(context=ctxs), urllib.request.HTTPCookieProcessor(jar))

def api(path, body=None, method=None):
    data = json.dumps(body).encode() if body is not None else None
    r = urllib.request.Request(U + path, data=data, method=method or ("POST" if data else "GET"), headers={"content-type": "application/json"})
    return json.loads(op.open(r).read() or b"null")

async def idle(pg):
    await pg.wait_for_function("() => chatCur && !chatCur.busy && chatCur.messages.length >= 2 && !document.querySelector('#chat-live .working')", timeout=60000)
    await pg.wait_for_timeout(300)

async def menu(pg, label):
    await pg.click("#group-ctx button:has-text('%s')" % label)

async def main():
    api("/api/login", {"password": pw})
    node = [n for n in api("/api/state")["nodes"] if n.get("online")][0]["id"]
    async with async_playwright() as pwr:
        b = await pwr.chromium.launch(executable_path=os.environ.get("OUTPOST_CHROMIUM") or None)
        errs = []
        for scheme, vp in [("light", {"width": 1400, "height": 900}), ("dark", {"width": 1400, "height": 900}), ("phone", {"width": 390, "height": 844})]:
            print("pass", scheme, flush=True)
            tag = scheme + str(os.getpid())
            proj = api("/api/projects", {"name": "Sidebar proj " + tag})
            pid = proj["id"]
            api("/api/projects/" + pid, {"planner": "node:" + node})
            plain = api("/api/chats", {"text": "Plain question " + tag, "node": "auto"})["id"]
            api("/api/chats", {"text": "Project question one " + tag, "node": "planner", "project_id": pid})
            kw = dict(is_mobile=True, has_touch=True) if scheme == "phone" else {}
            ctx = await b.new_context(ignore_https_errors=True, viewport=vp, color_scheme="light" if scheme == "light" else "dark", device_scale_factor=2, **kw)
            pg = await ctx.new_page()
            pg.on("pageerror", lambda e: errs.append(str(e)))
            pg.on("console", lambda m: errs.append(m.text) if m.type == "error" and "401" not in m.text else None)
            await pg.goto(U + "/"); await pg.fill("#pw", pw); await pg.click("button[type=submit]")
            await pg.wait_for_selector("#side-tasks"); await pg.wait_for_timeout(800)
            phone = scheme == "phone"
            if phone: await pg.click("#side-open"); await pg.wait_for_timeout(300)
            # the sidebar lists the plain chat, not the project's conversation
            await pg.wait_for_selector("#side-tasks .side-chat")
            titles = await pg.locator("#side-tasks .side-chat .side-task-title").all_inner_texts()
            assert any("Plain question" in t for t in titles), titles
            assert not any("Project question" in t for t in titles), "project conversations are not in the sidebar: %s" % titles
            if phone: await pg.keyboard.press("Escape"); await pg.evaluate("closeSide()")
            await pg.goto(U + "/#/chat/" + plain); await pg.wait_for_selector(".chat-answer", timeout=30000)
            assert await pg.locator("#chat-panel, #chat-panel-btn, #chat-list, #chat-find").count() == 0, "no right-hand Chats panel"
            if not phone:
                assert await pg.locator("#side-tasks .side-chat.active").count() == 1, "the open chat is marked in the sidebar"
                await pg.screenshot(path=S + "chat-sidebar-%s.png" % scheme)
            # move it into the project from the header menu
            await pg.click("#chat-more"); await menu(pg, "Move to project")
            await pg.click("#group-sub [data-mvp='%s']" % pid)
            await pg.wait_for_function("(p) => location.hash.startsWith('#/projects/' + p + '/chats/')", arg=pid, timeout=10000)
            await pg.wait_for_selector("#pj-main, .pj-main #chat-main", timeout=10000)
            await pg.wait_for_function("() => chatCur && chatCur.project_id", timeout=10000)
            assert await pg.locator("#pj-mode").is_visible(), "the project composer has Ask | Task | Research"
            if not await pg.locator("#pj-wrap.panel-open").count(): await pg.click("#pj-threads-btn")
            await pg.wait_for_selector("#pj-threads .pj-conv-row")
            n = await pg.locator("#pj-threads .pj-conv-row").count()
            assert n == 2, "the moved chat and the other conversation: %d" % n
            if not phone: await pg.screenshot(path=S + "project-conversations-%s.png" % scheme)
            # a second, new conversation
            await pg.click("[data-act=proj-conv-new]")
            await pg.wait_for_function("() => location.hash.endsWith('/chats/new')")
            await pg.wait_for_selector("#chat-msgs .pj-empty")
            if phone and await pg.locator("#pj-wrap.panel-open").count(): await pg.click("#pj-panel-close")
            assert await pg.input_value("#chat-node") == "planner", "the project's planner is the default"
            await pg.fill("#chat-text", "Second conversation " + scheme)
            await pg.click("#chat-send")
            await pg.wait_for_function("(p) => /\\/chats\\/chat_/.test(location.hash)", arg=pid, timeout=10000)
            await idle(pg)
            second = await pg.evaluate("chatCur.id")
            if not await pg.locator("#pj-wrap.panel-open").count(): await pg.click("#pj-threads-btn")
            await pg.wait_for_function("() => document.querySelectorAll('#pj-threads .pj-conv-row').length >= 3", timeout=10000)
            # switch between them
            async def pane():
                if not await pg.locator("#pj-wrap.panel-open").count(): await pg.click("#pj-threads-btn")
                await pg.wait_for_selector("#pj-threads .pj-conv-row")
            await pane()
            await pg.click("#pj-threads .pj-conv-row:has-text('Project question one') .pj-row-main")
            await pg.wait_for_function("() => chatCur && chatCur.title.startsWith('Project question one')", timeout=10000)
            assert "Second conversation" not in await pg.inner_text("#chat-msgs")
            await pane()
            await pg.click("#pj-threads .pj-conv-row[data-chat='%s'] .pj-row-main" % second)
            await pg.wait_for_function("(id) => chatCur && chatCur.id === id", arg=second, timeout=10000)
            assert "Second conversation" in await pg.inner_text("#chat-msgs")
            # Task: start a task from the text; the card is under the message
            await pg.click("#pj-mode [data-mode=task]")
            assert (await pg.inner_text("#chat-send")).strip() == "Start task"
            await pg.fill("#chat-text", "Write hello.py that prints hello")
            await pg.click("#chat-send")
            await pg.wait_for_selector("#chat-msgs .pj-tcard", timeout=30000)
            await pg.click("#pj-mode [data-mode=ask]")
            await pg.screenshot(path=S + "project-task-card-%s.png" % scheme)
            # remove it from the project: it shows in the sidebar again
            await pg.click("#chat-more"); await menu(pg, "Remove from project")
            await pg.wait_for_function("(id) => chatWant !== id", arg=second, timeout=10000)
            if not phone:
                await pg.wait_for_selector("#side-tasks .side-chat:has-text('Second conversation')", timeout=10000)
            await ctx.close()
        await b.close()
    assert not errs, errs
    print("chatsidebar ok", S)

asyncio.run(main())
