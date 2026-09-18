import http from "node:http";
import { readFile } from "node:fs/promises";
import { chromium } from "playwright";

// Test-owned browser and server share one cleanup path, including launch failure.
export async function createBrowserPage(t, routes = {}) {
  const assets = new URL("../dist/", import.meta.url);
  const server = http.createServer(async (request, response) => {
    try {
      const name = new URL(request.url, "http://localhost").pathname;
      if (name === "/") {
        response.setHeader("Content-Type", "text/html");
        response.end("<!doctype html>");
        return;
      }
      let source = routes[name];
      if (!source) {
        source = new URL(`.${name}`, assets);
        if (!source.pathname.startsWith(assets.pathname)) throw new Error("invalid asset path");
      }
      response.setHeader(
        "Content-Type",
        name.endsWith(".js")
          ? "text/javascript"
          : name.endsWith(".wasm")
            ? "application/wasm"
            : "application/octet-stream",
      );
      response.end(source instanceof URL ? await readFile(source) : source);
    } catch (error) {
      response.statusCode = 404;
      response.end(String(error));
    }
  });
  let browser;
  t.after(async () => {
    await browser?.close();
    server.closeAllConnections();
    if (server.listening) await new Promise((resolve) => server.close(resolve));
  });
  t.signal.addEventListener(
    "abort",
    () => {
      void browser?.close();
      server.closeAllConnections();
      server.close();
    },
    { once: true },
  );
  await new Promise((resolve) => server.listen(0, "127.0.0.1", resolve));
  browser = await chromium.launch({ headless: true });
  if (t.signal.aborted) {
    await browser.close();
    t.signal.throwIfAborted();
  }
  const page = await browser.newPage();
  await page.goto(`http://127.0.0.1:${server.address().port}`);
  return page;
}
