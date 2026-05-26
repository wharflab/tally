package io.github.wharflab.tally.toolchain

import java.nio.file.Path
import java.util.concurrent.TimeUnit

private val PROCESS_TIMEOUT_MINUTES = 30L

internal fun runProcess(vararg command: String) {
    val process = ProcessBuilder(*command)
        .redirectOutput(ProcessBuilder.Redirect.INHERIT)
        .redirectError(ProcessBuilder.Redirect.INHERIT)
        .start()
    val exit = waitWithTimeout(process, command)
    check(exit == 0) {
        "command failed (exit=$exit): ${command.joinToString(" ")}"
    }
}

/**
 * Wait for [process] up to [PROCESS_TIMEOUT_MINUTES]; on timeout
 * [Process.destroyForcibly] is called and the function throws. Returns the
 * exit code on success. Use this from any caller that builds a `ProcessBuilder`
 * directly (e.g. when output redirection rules out [runProcess]).
 */
internal fun waitWithTimeout(process: Process, command: Array<out String>): Int {
    val finished = process.waitFor(PROCESS_TIMEOUT_MINUTES, TimeUnit.MINUTES)
    if (!finished) {
        process.destroyForcibly()
        error("command timed out after ${PROCESS_TIMEOUT_MINUTES}m: ${command.joinToString(" ")}")
    }
    return process.exitValue()
}

/**
 * Find the actual IDE root inside an extracted distribution archive. The
 * tarball/zip wraps a single top-level directory (e.g. `WebStorm-252.26830.93`)
 * which is the real IDE home — we identify it by the presence of bin/, lib/,
 * and plugins/ subdirectories.
 */
internal fun locateIdeRoot(extractDir: Path): Path = extractDir.toFile()
    .walkTopDown()
    .maxDepth(6)
    .firstOrNull {
        it.isDirectory &&
            java.io.File(it, "bin").isDirectory &&
            java.io.File(it, "lib").isDirectory &&
            java.io.File(it, "plugins").isDirectory
    }
    ?.toPath()
    ?: error("unable to locate IDE home under $extractDir")
