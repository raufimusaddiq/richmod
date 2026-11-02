// Copies the Next static export into the Go module that embeds it.
import { cp, mkdir, rm, writeFile } from "node:fs/promises";

const from = new URL("../out/", import.meta.url);
const to = new URL("../../api/internal/webui/assets/", import.meta.url);

await rm(to, { recursive: true, force: true });
await mkdir(to, { recursive: true });
await cp(from, to, { recursive: true });
await writeFile(new URL(".gitkeep", to), "");
await writeFile(new URL(".gitignore", to), "*\n!.gitkeep\n!.gitignore\n");
