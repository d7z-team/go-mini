import init from "./wasm/mini_go_wasm.js";
import { runWorker } from "./worker.js";
import type { Request } from "./protocol.js";

runWorker(
  {
    send: (message) => globalThis.postMessage(message),
    listen: (handler) => {
      globalThis.onmessage = (event: MessageEvent<Request>) => handler(event.data);
    },
  },
  (url) => init({ module_or_path: url ?? new URL("./wasm/mini_go_wasm_bg.wasm", import.meta.url) }),
);
