import { createRequire } from "node:module";
import { resolve } from "node:path";

const require = createRequire(import.meta.url);

export const component = file => require(resolve(file));
