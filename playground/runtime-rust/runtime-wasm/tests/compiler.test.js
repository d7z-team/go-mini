import test from "node:test";
import { readFile } from "node:fs/promises";
import { gunzipSync } from "node:zlib";
import { MiniGo, values } from "@d7z-team/mini-go";
import { exerciseCompiler } from "./compiler_scenario.js";
import { createBrowserPage } from "./browser_helpers.js";

const compilerImage = new URL("../dist/tools/compiler.json.gz", import.meta.url);

test(
  "compiler workload: Node gzip image initialization and repeated source checks",
  { timeout: 60_000 },
  async () => {
    await exerciseCompiler(MiniGo, values, await readFile(compilerImage));
  },
);

test(
  "compiler workload: browser image initialization and repeated source checks",
  { timeout: 60_000 },
  async (t) => {
    const page = await createBrowserPage(t, {
      "/scenario.js": new URL("./compiler_scenario.js", import.meta.url),
      "/image": gunzipSync(await readFile(compilerImage)),
    });
    await page.evaluate(async () => {
      const { MiniGo, values } = await import("/browser.js");
      const { exerciseCompiler } = await import("/scenario.js");
      await exerciseCompiler(
        MiniGo,
        values,
        new Uint8Array(await (await fetch("/image")).arrayBuffer()),
      );
    });
  },
);
