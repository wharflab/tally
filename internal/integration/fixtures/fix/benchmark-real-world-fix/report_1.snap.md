Fixed 147 issues
Skipped 43 fixes
note: 3 AI fix(es) failed (see details below)
note: skipped fix hadolint/DL4001 (<stdin>): resolver not registered: ai-autofix
note: skipped fix hadolint/DL4001 (<stdin>): resolver not registered: ai-autofix
note: skipped fix hadolint/DL4001 (<stdin>): resolver not registered: ai-autofix
**121 issues** in `<stdin>`

| Line | Issue |
|------|-------|
| 5 | ⚠️ remove stale wget install and config after switching to curl |
| 117 | ⚠️ do not use apt as it is meant to be an end-user tool, use apt-get or apt-cache instead |
| 130 | ⚠️ set the SHELL option -o pipefail before RUN with a pipe in it |
| 148 | ⚠️ Quote this to prevent word splitting. |
| 150 | ⚠️ use WORKDIR to switch to a directory |
| 152 | ⚠️ use WORKDIR to switch to a directory |
| 152 | ⚠️ both wget and curl are installed; keep curl and remove wget |
| 152 | ⚠️ Quote this to prevent word splitting. |
| 232 | ⚠️ use WORKDIR to switch to a directory |
| 232 | ⚠️ prefer ADD <git source> over git clone in RUN for more hermetic, supply-chain-friendly builds |
| 238 | ⚠️ set the SHELL option -o pipefail before RUN with a pipe in it |
| 238 | ⚠️ final stage runs as root (no USER instruction (defaults to root)) and signals persistent state via RUN mkdir /var/run/sshd |
| 252 | ⚠️ use WORKDIR to switch to a directory |
| 269 | ⚠️ use WORKDIR to switch to a directory |
| 269 | ⚠️ both wget and curl are installed; keep curl and remove wget |
| 278 | ⚠️ both wget and curl are installed; keep curl and remove wget |
| 12 | ℹ️ echo may not expand escape sequences. Use printf. |
| 15 | ℹ️ echo may not expand escape sequences. Use printf. |
| 45 | ℹ️ Ranges can only match single chars (mentioned due to duplicates). |
| 64 | ℹ️ use cache mounts for package manager cache directories |
| 130 | ℹ️ use cache mounts for package manager cache directories |
| 150 | ℹ️ use `ADD --unpack <url> <dest>` instead of downloading and extracting in `RUN` |
| 173 | ℹ️ echo may not expand escape sequences. Use printf. |
| 176 | ℹ️ echo may not expand escape sequences. Use printf. |
| 252 | ℹ️ wget without progress bar will bloat build logs; use `wget --progress=dot:giga`, `-q`, or `-nv` |
| 252 | ℹ️ use `ADD --unpack <url> <dest>` instead of downloading and extracting in `RUN` |
| 264 | ℹ️ use cache mounts for package manager cache directories |
| 269 | ℹ️ use `ADD --unpack <url> <dest>` instead of downloading and extracting in `RUN` |
| 5 | 💅 split chained commands onto separate lines |
| 8 | 💅 expected blank line between ARG and RUN |
| 11 | 💅 expected blank line between ARG and RUN |
| 11 | 💅 split chained commands onto separate lines |
| 29 | 💅 split chained commands onto separate lines |
| 29 | 💅 multiple consecutive spaces (1 extra) |
| 35 | 💅 expected blank line between ARG and RUN |
| 35 | 💅 multiple consecutive spaces (18 extra) |
| 37 | 💅 unexpected blank line between RUN and RUN |
| 39 | 💅 unexpected blank line between RUN and RUN |
| 39 | 💅 split chained commands onto separate lines |
| 39 | 💅 RUN instruction with chained commands can use heredoc syntax |
| 39 | 💅 multiple consecutive spaces (1 extra) |
| 44 | 💅 expected blank line between ARG and RUN |
| 58 | 💅 expected blank line between COPY and RUN |
| 61 | 💅 expected blank line between ARG and RUN |
| 64 | 💅 expected blank line between ARG and RUN |
| 70 | 💅 unexpected blank line between ARG and ARG |
| 72 | 💅 unexpected blank line between ARG and ARG |
| 75 | 💅 expected blank line between ENV and ARG |
| 79 | 💅 unexpected blank line between ARG and ARG |
| 84 | 💅 unexpected blank line between ENV and ENV |
| 87 | 💅 unexpected blank line between ENV and ENV |
| 89 | 💅 unexpected blank line between ENV and ENV |
| 91 | 💅 unexpected blank line between ENV and ENV |
| 93 | 💅 unexpected blank line between ENV and ENV |
| 95 | 💅 unexpected blank line between ENV and ENV |
| 103 | 💅 unexpected blank line between ENV and ENV |
| 130 | 💅 expected blank line between ENV and RUN |
| 130 | 💅 split chained commands onto separate lines |
| 130 | 💅 packages in apt-get install are not sorted alphabetically |
| 130 | 💅 multiple consecutive spaces (20 extra) |
| 134 | 💅 expected blank line between ENV and RUN |
| 134 | 💅 split chained commands onto separate lines |
| 134 | 💅 multiple consecutive spaces (12 extra) |
| 136 | 💅 unexpected blank line between RUN and RUN |
| 142 | 💅 split chained commands onto separate lines |
| 142 | 💅 multiple consecutive spaces (4 extra) |
| 145 | 💅 expected blank line between ARG and RUN |
| 145 | 💅 split chained commands onto separate lines |
| 145 | 💅 multiple consecutive spaces (172 extra) |
| 148 | 💅 unexpected blank line between RUN and RUN |
| 148 | 💅 multiple consecutive spaces (4 extra) |
| 150 | 💅 unexpected blank line between RUN and RUN |
| 150 | 💅 split chained commands onto separate lines |
| 150 | 💅 multiple consecutive spaces (11 extra) |
| 152 | 💅 unexpected blank line between RUN and RUN |
| 157 | 💅 multiple consecutive spaces (5 extra) |
| 161 | 💅 split chained commands onto separate lines |
| 161 | 💅 multiple consecutive spaces (108 extra) |
| 167 | 💅 expected blank line between ARG and RUN |
| 169 | 💅 unexpected blank line between RUN and RUN |
| 172 | 💅 expected blank line between ARG and RUN |
| 172 | 💅 split chained commands onto separate lines |
| 172 | 💅 RUN instruction with chained commands can use heredoc syntax |
| 188 | 💅 unexpected blank line between RUN and RUN |
| 188 | 💅 multiple consecutive spaces (49 extra) |
| 191 | 💅 unexpected blank line between RUN and RUN |
| 197 | 💅 expected 1 blank line between COPY and ARG, found 2 |
| 207 | 💅 split chained commands onto separate lines |
| 207 | 💅 RUN instruction with chained commands can use heredoc syntax |
| 207 | 💅 multiple consecutive spaces (1 extra) |
| 210 | 💅 expected blank line between ARG and RUN |
| 215 | 💅 expected blank line between COPY and RUN |
| 223 | 💅 expected blank line between ARG and RUN |
| 230 | 💅 unexpected blank line between RUN and RUN |
| 234 | 💅 unexpected blank line between RUN and RUN |
| 236 | 💅 unexpected blank line between RUN and RUN |
| 236 | 💅 multiple consecutive spaces (16 extra) |
| 238 | 💅 unexpected blank line between RUN and RUN |
| 238 | 💅 split chained commands onto separate lines |
| 238 | 💅 multiple consecutive spaces (9 extra) |
| 240 | 💅 unexpected blank line between RUN and RUN |
| 244 | 💅 expected blank line between ARG and RUN |
| 246 | 💅 unexpected blank line between RUN and RUN |
| 248 | 💅 unexpected blank line between RUN and RUN |
| 248 | 💅 multiple consecutive spaces (8 extra) |
| 252 | 💅 RUN instruction with chained commands can use heredoc syntax |
| 263 | 💅 expected 1 blank line between WORKDIR and ARG, found 2 |
| 264 | 💅 expected blank line between ARG and RUN |
| 264 | 💅 multiple consecutive spaces (8 extra) |
| 266 | 💅 unexpected blank line between RUN and RUN |
| 266 | 💅 multiple consecutive spaces (14 extra) |
| 269 | 💅 expected blank line between ARG and RUN |
| 272 | 💅 expected 1 blank line between RUN and ENV, found 2 |
| 274 | 💅 split chained commands onto separate lines |
| 274 | 💅 multiple consecutive spaces (7 extra) |
| 278 | 💅 RUN instruction with chained commands can use heredoc syntax |
| 284 | 💅 unexpected blank line between RUN and RUN |
| 291 | 💅 expected 1 blank line between CMD and RUN, found 2 |
| 291 | 💅 split chained commands onto separate lines |
| 291 | 💅 multiple consecutive spaces (4 extra) |
| 293 | 💅 unexpected blank line between RUN and RUN |
