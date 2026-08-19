#!/usr/bin/env node
import { runCli } from "../src/cli.js";

runCli(process.argv.slice(2))
  .then((code) => {
    process.exitCode = code;
  })
  .catch((err) => {
    process.stderr.write(`agent-kit: ${err.message}\n`);
    process.exitCode = 1;
  });
