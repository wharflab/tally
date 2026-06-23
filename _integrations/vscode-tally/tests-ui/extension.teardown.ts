import { rmSync } from "node:fs";
import { join } from "node:path";

import { test as teardown } from "@playwright/test";

teardown("remove .test_setup scratch dir", () => {
  rmSync(join(__dirname, "..", ".test_setup"), { recursive: true, force: true });
});
