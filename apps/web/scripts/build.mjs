// Build entry point: derives the release id once, hands it to Next so every
// page carries it in a meta tag, then stamps the service worker with the same
// value.
import { spawn } from "node:child_process";
import { fileURLToPath } from "node:url";
import { buildId } from "./build-id.mjs";

const root = fileURLToPath(new URL("../", import.meta.url));
const id = await buildId();

function run(command, args, env = {}) {
  return new Promise((resolve, reject) => {
    const child = spawn(command, args, { cwd: root, stdio: "inherit", env: { ...process.env, ...env } });
    child.on("exit", code => (code === 0 ? resolve() : reject(new Error(`${command} exited with ${code}`))));
  });
}

await run(process.execPath, [fileURLToPath(new URL("../node_modules/next/dist/bin/next", import.meta.url)), "build"], {
  NEXT_PUBLIC_RICHMOD_BUILD: id,
  NEXT_TELEMETRY_DISABLED: process.env.NEXT_TELEMETRY_DISABLED || "1",
});
await run(process.execPath, [fileURLToPath(new URL("./build-service-worker.mjs", import.meta.url))], { RICHMOD_BUILD_ID: id });
