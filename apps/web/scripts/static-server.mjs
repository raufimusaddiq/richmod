import { fileURLToPath } from "node:url";
import { serveDirectory } from "./lib/static-handler.mjs";

const root = fileURLToPath(new URL("../out/", import.meta.url));
const portFlag = process.argv.indexOf("-p");
const port = Number(process.env.PORT || (portFlag >= 0 ? process.argv[portFlag + 1] : 3000));

serveDirectory(root, port, "static site");
