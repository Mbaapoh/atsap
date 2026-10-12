// Raises the console coverage floors in quality/coverage-floors.json to
// what the last `npm run coverage` measured (coverage/coverage-summary.json),
// rounded down to one decimal. Never lowers a floor (D-71).
import { readFileSync, writeFileSync } from "node:fs";

const floorsPath = new URL("../../quality/coverage-floors.json", import.meta.url);
const summary = JSON.parse(readFileSync(new URL("../coverage/coverage-summary.json", import.meta.url), "utf8")).total;
const floors = JSON.parse(readFileSync(floorsPath, "utf8"));

for (const metric of ["statements", "branches", "functions", "lines"]) {
  const measured = Math.floor(summary[metric].pct * 10) / 10;
  floors.console[metric] = Math.max(floors.console[metric] ?? 0, measured);
}
writeFileSync(floorsPath, JSON.stringify(floors, null, 2) + "\n");
console.log("console floors:", floors.console);
