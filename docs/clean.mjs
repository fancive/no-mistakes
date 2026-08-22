import { rmSync } from "node:fs";
import { fileURLToPath } from "node:url";

for (const name of [".astro", "dist", "node_modules/.astro"]) {
  rmSync(fileURLToPath(new URL(`./${name}`, import.meta.url)), {
    recursive: true,
    force: true,
  });
}
