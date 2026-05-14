Fixed 6 issues
**59 issues** in `<stdin>`

| Line | Issue |
|------|-------|
| - | ℹ️ `HEALTHCHECK` instruction missing |
| 17 | 💅 unexpected blank line between ARG and ARG |
| 19 | 💅 unexpected blank line between ARG and ARG |
| 21 | 💅 unexpected blank line between ARG and ARG |
| 23 | 💅 unexpected blank line between ARG and ARG |
| 25 | 💅 unexpected blank line between ARG and ARG |
| 27 | 💅 unexpected blank line between ARG and ARG |
| 29 | 💅 unexpected blank line between ARG and ARG |
| 31 | 💅 unexpected blank line between ARG and ARG |
| 33 | 💅 unexpected blank line between ARG and ARG |
| 38 | 💅 unexpected blank line between ARG and ARG |
| 46 | 💅 unexpected blank line between ARG and ARG |
| 48 | 💅 unexpected blank line between ARG and ARG |
| 51 | ❌ file has 261 lines, maximum allowed is 50 |
| 64 | ⚠️ stage "rust" (index 1) is not reachable from the final stage and does not contribute to the final image |
| 86 | ⚠️ stage "binaryen" (index 3) is not reachable from the final stage and does not contribute to the final image |
| 90 | 💅 unexpected blank line between ARG and ARG |
| 115 | ⚠️ stage "cargo-audit" (index 4) is not reachable from the final stage and does not contribute to the final image |
| 124 | ⚠️ stage "cargo-each" (index 5) is not reachable from the final stage and does not contribute to the final image |
| 131 | ⚠️ stage "cargo-udeps" (index 6) is not reachable from the final stage and does not contribute to the final image |
| 141 | ⚠️ stage "cosmwasm-check" (index 7) is not reachable from the final stage and does not contribute to the final image |
| 159 | ⚠️ stage "rust-ci" (index 9) is not reachable from the final stage and does not contribute to the final image |
| 168 | ⚠️ stage "rust-ci-multi-workspace" (index 10) is not reachable from the final stage and does not contribute to the final image |
| 176 | ⚠️ stage "audit-dependencies" (index 11) is not reachable from the final stage and does not contribute to the final image |
| 186 | ⚠️ stage "check-formatting" (index 12) is not reachable from the final stage and does not contribute to the final image |
| 188 | 💅 epilogue instructions should appear at the end of the stage in order: STOPSIGNAL, HEALTHCHECK, ENTRYPOINT, CMD |
| 192 | ⚠️ stage "check-lockfiles" (index 13) is not reachable from the final stage and does not contribute to the final image |
| 194 | 💅 epilogue instructions should appear at the end of the stage in order: STOPSIGNAL, HEALTHCHECK, ENTRYPOINT, CMD |
| 202 | ⚠️ stage "check-unused-dependencies" (index 14) is not reachable from the final stage and does not contribute to the final image |
| 210 | 💅 epilogue instructions should appear at the end of the stage in order: STOPSIGNAL, HEALTHCHECK, ENTRYPOINT, CMD |
| 218 | 💅 unexpected blank line between COPY and COPY |
| 224 | 💅 unexpected blank line between COPY and COPY |
| 230 | ⚠️ stage "lint" (index 15) is not reachable from the final stage and does not contribute to the final image |
| 234 | 💅 epilogue instructions should appear at the end of the stage in order: STOPSIGNAL, HEALTHCHECK, ENTRYPOINT, CMD |
| 242 | 💅 unexpected blank line between COPY and COPY |
| 248 | ⚠️ stage "test" (index 16) is not reachable from the final stage and does not contribute to the final image |
| 250 | 💅 epilogue instructions should appear at the end of the stage in order: STOPSIGNAL, HEALTHCHECK, ENTRYPOINT, CMD |
| 258 | 💅 unexpected blank line between COPY and COPY |
| 264 | ⚠️ stage "build" (index 17) is not reachable from the final stage and does not contribute to the final image |
| 276 | 💅 unexpected blank line between ONBUILD and ONBUILD |
| 286 | 💅 unexpected blank line between ARG and ARG |
| 288 | 💅 unexpected blank line between ARG and ARG |
| 290 | 💅 unexpected blank line between ARG and ARG |
| 292 | 💅 unexpected blank line between ARG and ARG |
| 294 | 💅 unexpected blank line between ARG and ARG |
| 296 | 💅 unexpected blank line between ARG and ARG |
| 298 | 💅 unexpected blank line between ARG and ARG |
| 315 | 💅 unexpected blank line between COPY and COPY |
| 321 | 💅 unexpected blank line between COPY and COPY |
| 327 | 💅 unexpected blank line between COPY and COPY |
| 333 | 💅 unexpected blank line between COPY and COPY |
| 339 | 💅 unexpected blank line between COPY and COPY |
| 345 | ⚠️ stage "build-platform" (index 18) is not reachable from the final stage and does not contribute to the final image |
| 357 | ⚠️ stage "build-protocol" (index 19) is not reachable from the final stage and does not contribute to the final image |
| 379 | ⚠️ stage "compress" (index 20) is not reachable from the final stage and does not contribute to the final image |
| 381 | 💅 epilogue instructions should appear at the end of the stage in order: STOPSIGNAL, HEALTHCHECK, ENTRYPOINT, CMD |
| 391 | ⚠️ final stage runs as root (no USER instruction (defaults to root)) and signals persistent state via volume /bind |
| 393 | 💅 epilogue instructions should appear at the end of the stage in order: STOPSIGNAL, HEALTHCHECK, ENTRYPOINT, CMD |
| 403 | 💅 unexpected blank line between COPY and COPY |
